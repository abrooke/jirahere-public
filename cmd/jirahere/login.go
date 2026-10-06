package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/aslanbrooke/jirahere/internal/acli"
	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
	"github.com/aslanbrooke/jirahere/internal/layout"
)

const configPathDisplay = "~/.config/jirahere/config.json"

var validateAPIToken = auth.ValidateAPIToken

func profileConfigPathDisplay(name string) string {
	if name == "" {
		return configPathDisplay
	}
	return "~/.config/" + layout.ProfilesDirName + "/" + sanitizeSingleLine(name) + "/config.json"
}

func profileSuffix(name string) string {
	if name == "" {
		return ""
	}
	return fmt.Sprintf(" (profile %q)", sanitizeSingleLine(name))
}

var acliAvailable = acli.Available

const (
	acliUnsupportedNotice = "jirahere: acli is installed but is not supported as a Jira provider: acli cannot set a parent on an existing work item. jirahere uses its own credentials."
	acliLoginPointer      = `Run "jirahere login --api-token" to log in with jirahere's own credentials.`
)

type errSilent struct{ err error }

func (e *errSilent) Error() string { return e.err.Error() }
func (e *errSilent) Unwrap() error { return e.err }

func failf(format string, args ...any) error {
	return emitSilent(format, args...)
}

func emitSilent(format string, args ...any) *errSilent {
	msg := sanitizeSingleLine(fmt.Sprintf(format, args...))
	fmt.Fprintln(os.Stderr, msg)
	return &errSilent{err: fmt.Errorf("%s", msg)}
}

type errUsage struct{ silent *errSilent }

func (e *errUsage) Error() string { return e.silent.Error() }
func (e *errUsage) Unwrap() error { return e.silent }

func usagef(format string, args ...any) error {
	return &errUsage{silent: emitSilent(format, args...)}
}

type flagUsageSpec struct {
	Positionals []string
}

func parseFlagSet(fs *flag.FlagSet, args []string, spec ...flagUsageSpec) error {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(os.Stdout)
			if werr := writeFlagUsage(fs.Output(), fs, spec...); werr != nil {

				return werr
			}
			return flag.ErrHelp
		}
		return usagef("jirahere: %s", twoDashFlagName(fs, err.Error()))
	}
	return nil
}

func twoDashFlagName(fs *flag.FlagSet, msg string) string {
	for _, prefix := range []string{
		"flag needs an argument: -",
		"flag provided but not defined: -",
	} {
		if rest, ok := strings.CutPrefix(msg, prefix); ok {
			return prefix + "-" + rest
		}
	}
	for _, shape := range []struct{ head, mid string }{
		{"invalid value ", " for flag -"},
		{"invalid boolean value ", " for -"},
	} {
		rest, ok := strings.CutPrefix(msg, shape.head)
		if !ok {
			continue
		}
		quoted, err := strconv.QuotedPrefix(rest)
		if err != nil {
			continue
		}
		afterQuoted, ok := strings.CutPrefix(rest[len(quoted):], shape.mid)
		if !ok {
			continue
		}
		name, tail, ok := strings.Cut(afterQuoted, ": ")
		if !ok || fs.Lookup(name) == nil {
			continue
		}
		return shape.head + quoted + shape.mid + "-" + name + ": " + tail
	}
	return msg
}

func writeFlagUsage(w io.Writer, fs *flag.FlagSet, spec ...flagUsageSpec) error {
	var flags []*flag.Flag
	fs.VisitAll(func(f *flag.Flag) { flags = append(flags, f) })

	name := fs.Name()
	if len(spec) > 0 {
		for _, p := range spec[0].Positionals {
			name += " " + p
		}
	}

	var b strings.Builder
	if len(flags) == 0 {
		fmt.Fprintf(&b, "usage: jirahere %s\n", name)
	} else {
		fmt.Fprintf(&b, "usage: jirahere %s [options]\n\noptions:\n", name)
		for _, f := range flags {
			name, usage := flag.UnquoteUsage(f)
			b.WriteString("  --")
			b.WriteString(f.Name)
			if name != "" {
				b.WriteString(" ")
				b.WriteString(name)
			}
			b.WriteString("\n")
			if detail := usage + flagDefaultNote(f); detail != "" {
				b.WriteString("        ")
				b.WriteString(strings.ReplaceAll(detail, "\n", "\n        "))
				b.WriteString("\n")
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func flagDefaultNote(f *flag.Flag) string {
	typ := reflect.TypeOf(f.Value)
	var zero reflect.Value
	if typ.Kind() == reflect.Pointer {
		zero = reflect.New(typ.Elem())
	} else {
		zero = reflect.Zero(typ)
	}
	if z, ok := zero.Interface().(flag.Value); ok && f.DefValue == z.String() {
		return ""
	}
	if typ.Kind() == reflect.Pointer && typ.Elem().Kind() == reflect.String {
		return fmt.Sprintf(" (default %q)", f.DefValue)
	}
	return fmt.Sprintf(" (default %v)", f.DefValue)
}

func runLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	apiToken := fs.Bool("api-token", false, "use API token auth instead of OAuth; also forces this path even when acli is present")
	site := fs.String("site", "", "Jira Cloud site, e.g. yourteam.atlassian.net")
	email := fs.String("email", "", "Atlassian account email (API token flow only)")
	debug := fs.Bool("debug", false, "print safe API request diagnostics to stderr (API token flow only)")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, args); err != nil {
		return err
	}

	prof, err := resolveProfile()
	if err != nil {
		return err
	}

	if acliAvailable() {
		fmt.Fprintln(os.Stderr, acliUnsupportedNotice)
		if !*apiToken {
			return failf("%s", acliLoginPointer)
		}
	}

	printExistingConfigNotice(prof)

	if *apiToken {
		return loginAPIToken(prof, *site, *email, *debug)
	}
	return loginOAuth(prof, *site)
}

func printExistingConfigNotice(prof string) {
	cfg, err := auth.Load(prof)
	if err != nil {
		return
	}
	fmt.Printf("Currently logged in to %s via %s%s. Continuing will replace this.\n", sanitizeSingleLine(cfg.Site), sanitizeSingleLine(auth.ProviderLabel(cfg.Provider)), profileSuffix(prof))
}

func loginAPIToken(prof, siteFlag, emailFlag string, debug bool) error {
	if debug {
		fmt.Fprintln(os.Stderr, "Debug mode enabled: request methods, response status codes, and body sizes will be printed to stderr. Credentials, URLs, headers, and response bodies are excluded.")
	}

	interactive := auth.IsTerminal(os.Stdin.Fd())
	reader := bufio.NewReader(os.Stdin)

	site, err := resolveField(reader, siteFlag, "Jira site (e.g. yourteam.atlassian.net)", "site", interactive)
	if err != nil {
		return err
	}
	if err := auth.ValidateSiteHostname(site); err != nil {
		return usagef("%s", err)
	}
	email, err := resolveField(reader, emailFlag, "Email", "email", interactive)
	if err != nil {
		return err
	}

	token, err := auth.ReadAPIToken(os.Stdin.Fd(), reader)
	if err != nil {
		return err
	}
	if token == "" {
		return failf("login: no API token supplied")
	}

	fmt.Println("Checking credentials...")

	var debugWriter io.Writer
	if debug {
		debugWriter = os.Stderr
	}
	me, err := validateAPIToken(context.Background(), site, email, token, debugWriter)
	if err != nil {
		return apiTokenLoginFailure(site, err)
	}

	cfg := &auth.Config{
		Provider: auth.ProviderAPIToken,
		Site:     site,
		APIToken: &auth.APITokenConfig{Email: email, Token: token},
	}
	if err := auth.Save(prof, cfg); err != nil {
		return err
	}

	fmt.Printf("Logged in to %s as %s <%s> using API token%s.\n", sanitizeSingleLine(site), sanitizeSingleLine(me.DisplayName), sanitizeSingleLine(me.EmailAddress), profileSuffix(prof))
	fmt.Printf("Credentials stored in %s.\n", profileConfigPathDisplay(prof))
	return nil
}

func sanitizeForTerminal(s string) string {
	return strings.Map(func(r rune) rune {

		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

func sanitizeSingleLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

func renderJiraError(err error) string {
	var statusErr *jira.StatusError
	if errors.As(err, &statusErr) && isHTTPStatusCode(statusErr.StatusCode) {
		return fmt.Sprintf("Jira returned an unexpected response (%d)", statusErr.StatusCode)
	}
	return "could not reach Jira"
}

func isHTTPStatusCode(status int) bool {
	return status >= 100 && status <= 599
}

func resolveField(reader *bufio.Reader, flagVal, label, flagName string, interactive bool) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if !interactive {
		return "", usagef("login: --%s is required when running non-interactively", flagName)
	}
	fmt.Printf("%s: ", label)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func apiTokenLoginFailure(site string, err error) error {
	var statusErr *jira.StatusError
	if errors.As(err, &statusErr) {
		if statusErr.StatusCode == 401 {
			return failf("Login failed: Jira rejected the email/token combination (401 Unauthorized). Check your email and API token and try again.")
		}
		if isHTTPStatusCode(statusErr.StatusCode) {
			return failf("Login failed: Jira returned an unexpected response (%d) from %s.", statusErr.StatusCode, site)
		}
		return failf("Login failed: Jira returned an unexpected response. Check the site URL and try again.")
	}

	var dnsErr *net.DNSError
	var opErr *net.OpError
	var syntaxErr *json.SyntaxError
	var unmarshalTypeErr *json.UnmarshalTypeError
	isSiteURLError := errors.As(err, &dnsErr) ||
		errors.As(err, &opErr) ||
		errors.As(err, &syntaxErr) ||
		errors.As(err, &unmarshalTypeErr)
	if isSiteURLError {
		return failf("Could not reach %q — check the site URL (it should look like yourteam.atlassian.net).", site)
	}

	return failf("Could not reach Atlassian to complete login. Check your network connection and try again.")
}

func loginOAuth(prof, siteFlag string) error {
	if auth.OAuthClientID == "" {
		return failf("login: OAuth is not configured for this build (no client ID). Use \"jirahere login --api-token\" instead.")
	}

	pkce, err := auth.NewAuthCodePKCE()
	if err != nil {
		return err
	}

	ln, err := auth.ListenLoopback()
	if err != nil {
		return failf("Could not start local login listener: port %d is in use. Close whatever's using it and try again.", auth.OAuthPort)
	}
	defer func() { _ = ln.Close() }()

	authURL := auth.BuildAuthorizeURL(auth.OAuthClientID, pkce)

	fmt.Println("Opening your browser to log in to Atlassian...")
	fmt.Println("If it doesn't open automatically, visit:")
	fmt.Printf("  %s\n\n", authURL)
	fmt.Println("Waiting for you to finish in the browser (up to 5 minutes)...")

	_ = auth.OpenBrowser(authURL)

	code, err := auth.AwaitCallback(ln, pkce.State)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrLoginTimeout):
			return failf(`Timed out waiting for browser authorization. If you're on a headless machine, use "jirahere login --api-token" instead.`)
		case errors.Is(err, auth.ErrLoginDenied):
			return failf("Login was cancelled or denied.")
		case errors.Is(err, auth.ErrInvalidCallback):
			return failf("Login failed (invalid callback). Try again.")
		default:
			return oauthLoginFailure("waiting for the browser callback", err)
		}
	}

	ctx := context.Background()
	tok, err := auth.ExchangeCode(ctx, auth.OAuthClientID, pkce.Verifier, code)
	if err != nil {
		return oauthLoginFailure("exchanging the authorization code", err)
	}

	if line := identityLine(jira.Me(ctx, tok.AccessToken)); line != "" {
		fmt.Println(line)
	}

	resources, err := jira.AccessibleResources(ctx, tok.AccessToken)
	if err != nil {
		return oauthLoginFailure("looking up your Jira sites", err)
	}

	sites := make([]auth.Site, len(resources))
	for i, r := range resources {
		sites[i] = auth.Site{CloudID: r.ID, URL: r.URL}
	}

	site, err := auth.SelectSite(sites, siteFlag, promptForSite)
	if err != nil {
		var mismatch *auth.ErrSiteMismatch
		switch {
		case errors.Is(err, auth.ErrNoSites):
			return failf("Your account doesn't have access to any Jira site. Ask a site admin for access and try again.")
		case errors.As(err, &mismatch):
			return failf("%q isn't one of the sites you authorized: %s.", mismatch.Requested, strings.Join(mismatch.Available, ", "))
		default:
			return err
		}
	}

	expiresAt := tokenExpiry(tok.ExpiresIn)
	cfg := &auth.Config{
		Provider: auth.ProviderOAuth,
		Site:     site.Hostname(),
		CloudID:  site.CloudID,
		OAuth: &auth.OAuthConfig{
			AccessToken:  tok.AccessToken,
			RefreshToken: tok.RefreshToken,
			ExpiresAt:    expiresAt,
		},
	}
	if err := auth.Save(prof, cfg); err != nil {
		return err
	}

	fmt.Printf("Logged in to %s using OAuth%s.\n", site.Hostname(), profileSuffix(prof))
	fmt.Printf("Credentials stored in %s.\n", profileConfigPathDisplay(prof))
	return nil
}

func oauthLoginFailure(stage string, err error) error {
	var statusErr *jira.StatusError
	if errors.As(err, &statusErr) && isHTTPStatusCode(statusErr.StatusCode) {
		return failf("Could not reach Atlassian to complete login: unexpected response (%d) while %s. Check your network connection and try again.", statusErr.StatusCode, stage)
	}
	var exchangeErr *auth.ExchangeStatusError
	if errors.As(err, &exchangeErr) && isHTTPStatusCode(exchangeErr.StatusCode) {
		return failf("Atlassian rejected the login request (%d) while %s. Try \"jirahere login\" again.", exchangeErr.StatusCode, stage)
	}
	return failf("Could not reach Atlassian to complete login while %s. Check your network connection and try again.", stage)
}

func identityLine(me *jira.Identity, err error) string {
	if err != nil {
		return ""
	}
	return fmt.Sprintf("Logged in as %s <%s>.", sanitizeSingleLine(me.Name), sanitizeSingleLine(me.Email))
}

func promptForSite(sites []auth.Site) (int, error) {
	fmt.Println("Multiple Jira sites are available to your account:")
	for i, s := range sites {
		fmt.Printf("  %d) %s\n", i+1, sanitizeSingleLine(s.Hostname()))
	}
	fmt.Printf("Select a site [1-%d]: ", len(sites))

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(sites) {
		return 0, fmt.Errorf("invalid selection %q", strings.TrimSpace(line))
	}
	return n - 1, nil
}

func tokenExpiry(expiresInSeconds int) time.Time {
	return time.Now().Add(time.Duration(expiresInSeconds) * time.Second)
}
