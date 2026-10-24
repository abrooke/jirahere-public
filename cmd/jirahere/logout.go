package main

import (
	"flag"
	"fmt"

	"github.com/aslanbrooke/jirahere/internal/auth"
	"github.com/aslanbrooke/jirahere/internal/layout"
	"github.com/aslanbrooke/jirahere/internal/quarter"
)

const cachePathDisplay = "~/.cache/jirahere"

func profileCachePathDisplay(name string) string {
	if name == "" {
		return cachePathDisplay
	}
	return "~/.cache/" + layout.ProfilesDirName + "/" + sanitizeSingleLine(name)
}

func runLogout(args []string) error {
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, args); err != nil {
		return err
	}

	prof, err := resolveProfile()
	if err != nil {
		return err
	}

	existed, err := auth.Delete(prof)
	if err != nil {
		return err
	}

	if existed {
		fmt.Printf("Logged out%s.\n", profileSuffix(prof))
	} else {
		fmt.Printf("Not logged in%s.\n", profileSuffix(prof))
	}

	if err := quarter.ClearCache(prof); err != nil {
		return failf("logout: credentials were removed, but the quarter inventory cache at %s could not be cleared. You are logged out; remove that directory by hand so a later login does not reuse this account's cached work items.", profileCachePathDisplay(prof))
	}
	return nil
}
