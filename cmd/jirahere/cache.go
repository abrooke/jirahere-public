package main

import (
	"flag"
	"fmt"

	"github.com/aslanbrooke/jirahere/internal/quarter"
)

const cacheClearUsage = "usage: jirahere cache clear [--profile <name>]"

var clearQuarterCache = quarter.ClearCache

func runCache(args []string) error {
	if hasHelpToken(args) {
		return runCacheClear([]string{"--help"})
	}
	if len(args) == 0 || args[0] != "clear" {
		return usagef(cacheClearUsage)
	}
	return runCacheClear(args[1:])
}

func runCacheClear(args []string) error {
	fs := flag.NewFlagSet("cache clear", flag.ContinueOnError)
	resolveProfile := registerProfileFlag(fs)
	if err := parseFlagSet(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return usagef(cacheClearUsage)
	}

	prof, err := resolveProfile()
	if err != nil {
		return err
	}

	if err := clearQuarterCache(prof); err != nil {
		return failf("jirahere: cache clear: could not clear the quarter inventory cache at %s. Remove that directory by hand.", profileCachePathDisplay(prof))
	}

	if _, err := fmt.Printf("Cache cleared%s.\n", profileSuffix(prof)); err != nil {
		return err
	}
	return nil
}
