package auth

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"
)

const (
	OAuthPort        = 8976
	oauthRedirectURI = "http://localhost:8976/callback"

	oauthAuthorizeURL = "https://auth.atlassian.com/authorize"

	oauthScopes = "read:jira-work write:jira-work read:jira-user read:me offline_access"

	oauthCallbackTimeout = 5 * time.Minute
)

var oauthTokenURL = "https://auth.atlassian.com/oauth/token"

var (
	ErrLoginDenied     = errors.New("login was cancelled or denied")
	ErrInvalidCallback = errors.New("invalid OAuth callback")
	ErrLoginTimeout    = errors.New("timed out waiting for browser authorization")
)

type RefreshStatusError struct {
	StatusCode int
}

func (e *RefreshStatusError) Error() string {
	return fmt.Sprintf("token refresh failed with status %d", e.StatusCode)
}

type ExchangeStatusError struct {
	StatusCode int
}

func (e *ExchangeStatusError) Error() string {
	return fmt.Sprintf("token exchange failed with status %d", e.StatusCode)
}

var OAuthClientID = ""

type AuthCodePKCE struct {
	Verifier  string
	Challenge string
	State     string
}

func NewAuthCodePKCE() (AuthCodePKCE, error) {
	verifier, err := generateVerifier()
	if err != nil {
		return AuthCodePKCE{}, err
	}
	state, err := generateState()
	if err != nil {
		return AuthCodePKCE{}, err
	}
	return AuthCodePKCE{
		Verifier:  verifier,
		Challenge: challengeFromVerifier(verifier),
		State:     state,
	}, nil
}

func BuildAuthorizeURL(clientID string, pkce AuthCodePKCE) string {
	q := url.Values{
		"audience":              {"api.atlassian.com"},
		"client_id":             {clientID},
		"scope":                 {oauthScopes},
		"redirect_uri":          {oauthRedirectURI},
		"state":                 {pkce.State},
		"response_type":         {"code"},
		"prompt":                {"consent"},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {"S256"},
	}
	return oauthAuthorizeURL + "?" + q.Encode()
}

func ListenLoopback() (net.Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", OAuthPort))
	if err != nil {
		return nil, fmt.Errorf("port %d is in use: %w", OAuthPort, err)
	}
	return ln, nil
}

func parseCallbackParams(q url.Values, expectedState string) (code string, err error) {
	if e := q.Get("error"); e != "" {
		return "", ErrLoginDenied
	}
	state := q.Get("state")
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(expectedState)) != 1 {
		return "", ErrInvalidCallback
	}
	code = q.Get("code")
	if code == "" {
		return "", ErrInvalidCallback
	}
	return code, nil
}

func AwaitCallback(ln net.Listener, expectedState string) (string, error) {
	type result struct {
		code string
		err  error
	}
	resultCh := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code, err := parseCallbackParams(r.URL.Query(), expectedState)
		if err != nil {
			_, _ = fmt.Fprintln(w, "Login failed. You can close this window and return to the terminal.")
		} else {
			_, _ = fmt.Fprintln(w, "Login complete. You can close this window and return to the terminal.")
		}
		resultCh <- result{code: code, err: err}
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() {

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	select {
	case res := <-resultCh:
		return res.code, res.err
	case <-time.After(oauthCallbackTimeout):
		return "", ErrLoginTimeout
	}
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func ExchangeCode(ctx context.Context, clientID, verifier, code string) (*TokenResponse, error) {
	payload := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     clientID,
		"code":          code,
		"redirect_uri":  oauthRedirectURI,
		"code_verifier": verifier,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("could not encode token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("could not build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {

		return nil, &ExchangeStatusError{StatusCode: resp.StatusCode}
	}

	var tok TokenResponse
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return nil, fmt.Errorf("could not parse token response: %w", err)
	}
	return &tok, nil
}

func RefreshToken(ctx context.Context, clientID, refreshToken string) (*TokenResponse, error) {
	payload := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     clientID,
		"refresh_token": refreshToken,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("could not encode token refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("could not build token refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {

		return nil, errors.New("OAuth token refresh failed")
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {

		return nil, errors.New("OAuth token refresh failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {

		return nil, &RefreshStatusError{StatusCode: resp.StatusCode}
	}

	var tok TokenResponse
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return nil, errors.New("OAuth token refresh failed")
	}
	return &tok, nil
}

func OpenBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}
