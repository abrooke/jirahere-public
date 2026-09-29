package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/aslanbrooke/jirahere/internal/adf"
	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/jira"
)

const commentAddUsage = "usage: jirahere comment add <id> --body <text> [--profile <name>]"

var commentAddValueFlags = map[string]bool{"body": true, "profile": true}

func splitCommentAddArgs(args []string) (positional []string, flagArgs []string) {
	terminated := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if terminated {
			positional = append(positional, a)
			continue
		}
		if a == "--" {
			terminated = true
			continue
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if commentAddValueFlags[name] && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	return positional, flagArgs
}

func runComment(args []string) error {
	if hasHelpToken(args) {
		if len(args) > 0 && args[0] == "list" {
			return runCommentList([]string{"--help"})
		}
		return runCommentAdd([]string{"--help"})
	}
	if len(args) == 0 || (args[0] != "add" && args[0] != "list") {
		return usagef("usage: jirahere comment <add|list> ...")
	}
	if args[0] == "list" {
		return runCommentList(args[1:])
	}
	return runCommentAdd(args[1:])
}

func runCommentAdd(args []string) error {
	var positional []string
	var flagArgs []string
	if hasHelpToken(args) {
		flagArgs = []string{"--help"}
	} else {
		positional, flagArgs = splitCommentAddArgs(args)
	}

	fs := flag.NewFlagSet("comment add", flag.ContinueOnError)
	body := fs.String("body", "", "plain-text comment body")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, flagArgs); err != nil {
		return err
	}
	if fs.NArg() != 0 || len(positional) != 1 || positional[0] == "" {
		return usagef(commentAddUsage)
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}
	if *body == "" {
		return usagef("jirahere: --body is required and must not be empty.")
	}

	provider, err := auth.LoadProvider(prof)
	if err != nil {
		if errors.Is(err, auth.ErrNotLoggedIn) {
			return failf("%s", notLoggedInMessage(prof))
		}
		return failf("jirahere: could not load Jira credentials. Run \"%s\" again.", loginRemediation(prof))
	}
	ctx := context.Background()
	creds, err := provider.Credentials(ctx)
	if err != nil {
		return refreshCredentialsFailureFor(err, prof)
	}
	var client jira.CommentService = jira.NewClient(creds.BaseURL, creds.AuthHeader)
	result, err := client.AddComment(ctx, positional[0], adf.FromPlainText(*body))
	if err != nil {
		return failf("jirahere: comment add failed: %s.", renderJiraError(err))
	}
	_, err = fmt.Println(sanitizeSingleLine(result.ID))
	return err
}

const commentListUsage = "usage: jirahere comment list <id> [--json] [--profile <name>]"

type commentListDoc struct {
	Comments []commentListItem `json:"comments"`
}

type commentListItem struct {
	ID      string `json:"id"`
	Author  string `json:"author"`
	Created string `json:"created"`
	Body    string `json:"body"`
}

var commentListValueFlags = map[string]bool{"profile": true}

func splitCommentListArgs(args []string) (positional []string, flagArgs []string) {
	terminated := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if terminated {
			positional = append(positional, a)
			continue
		}
		if a == "--" {
			terminated = true
			continue
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if commentListValueFlags[name] && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	return positional, flagArgs
}

func runCommentList(args []string) error {
	var positional []string
	var flagArgs []string
	if hasHelpToken(args) {
		flagArgs = []string{"--help"}
	} else {
		positional, flagArgs = splitCommentListArgs(args)
	}

	fs := flag.NewFlagSet("comment list", flag.ContinueOnError)
	jsonOutput := fs.Bool("json", false, "render JSON output")
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, flagArgs); err != nil {
		return err
	}
	if fs.NArg() != 0 || len(positional) != 1 || positional[0] == "" {
		return usagef(commentListUsage)
	}
	prof, err := resolveProfile()
	if err != nil {
		return err
	}
	key := positional[0]

	provider, err := auth.LoadProvider(prof)
	if err != nil {
		if errors.Is(err, auth.ErrNotLoggedIn) {
			return failf("%s", notLoggedInMessage(prof))
		}
		return failf("jirahere: could not load Jira credentials. Run \"%s\" again.", loginRemediation(prof))
	}
	ctx := context.Background()
	creds, err := provider.Credentials(ctx)
	if err != nil {
		return refreshCredentialsFailureFor(err, prof)
	}
	var client jira.CommentService = jira.NewClient(creds.BaseURL, creds.AuthHeader)
	comments, err := client.ListComments(ctx, key)
	if err != nil {
		return failf("jirahere: comment list failed: %s.", renderJiraError(err))
	}
	if *jsonOutput {
		return renderCommentListJSON(comments)
	}
	return renderCommentList(comments, key)
}

func renderCommentList(comments []jira.Comment, key string) error {
	if len(comments) == 0 {
		_, err := fmt.Printf("no comments on %s\n", sanitizeSingleLine(key))
		return err
	}
	for i, c := range comments {
		if i > 0 {
			if _, err := fmt.Println(); err != nil {
				return err
			}
		}
		if _, err := fmt.Printf("%s\t%s\n", sanitizeSingleLine(c.Author.DisplayName), sanitizeSingleLine(c.Created)); err != nil {
			return err
		}
		if _, err := fmt.Println(adf.PlainText(c.Body)); err != nil {
			return err
		}
	}
	return nil
}

func renderCommentListJSON(comments []jira.Comment) error {
	items := make([]commentListItem, 0, len(comments))
	for _, c := range comments {
		items = append(items, commentListItem{
			ID:      sanitizeSingleLine(c.ID),
			Author:  sanitizeSingleLine(c.Author.DisplayName),
			Created: sanitizeSingleLine(c.Created),
			Body:    adf.PlainText(c.Body),
		})
	}
	b, _ := json.MarshalIndent(commentListDoc{Comments: items}, "", "  ")
	_, err := fmt.Println(string(b))
	return err
}
