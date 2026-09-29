package main

import (
	"flag"

	"github.com/aslanbrooke/jirahere/internal/profile"
)

const profileFlagUsage = "run against the named `profile` (its own login, settings, and cache); omit for the default profile"

func registerProfileFlag(fs *flag.FlagSet) func() (string, error) {
	name := fs.String("profile", "", profileFlagUsage)
	return func() (string, error) {
		given := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "profile" {
				given = true
			}
		})
		if !given {
			return "", nil
		}
		if *name == "" {
			return "", usagef("jirahere: --profile was given but is empty; a profile name must not be empty.")
		}
		if err := profile.Validate(*name); err != nil {
			return "", usagef("jirahere: invalid --profile: %s.", err)
		}
		return *name, nil
	}
}

func loginRemediation(name string) string {
	if name == "" {
		return "jirahere login"
	}
	return "jirahere login --profile " + sanitizeSingleLine(name)
}

func notLoggedInMessage(name string) string {
	if name == "" {
		return `jirahere: not logged in. Run "` + loginRemediation(name) + `" first.`
	}
	return `jirahere: not logged in (profile "` + sanitizeSingleLine(name) + `"). Run "` + loginRemediation(name) + `" first.`
}
