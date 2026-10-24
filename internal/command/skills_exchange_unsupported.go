//go:build !linux && !darwin

package command

import "errors"

var errSkillDirectoryExchangeUnavailable = errors.New("atomic directory exchange is unavailable on this platform")

var exchangeSkillDirectories = func(stage, destination string) error {
	return errSkillDirectoryExchangeUnavailable
}
