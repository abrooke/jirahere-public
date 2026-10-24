//go:build linux

package command

import "golang.org/x/sys/unix"

var exchangeSkillDirectories = func(stage, destination string) error {
	return unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, destination, unix.RENAME_EXCHANGE)
}
