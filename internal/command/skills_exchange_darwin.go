//go:build darwin

package command

import (
	"syscall"
	"unsafe"
)

const (
	darwinRenameatxNP = 488
	darwinATFDCWD     = ^uintptr(1)
	darwinRenameSwap  = 0x2
)

var exchangeSkillDirectories = func(stage, destination string) error {
	from, err := syscall.BytePtrFromString(stage)
	if err != nil {
		return err
	}
	to, err := syscall.BytePtrFromString(destination)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(
		darwinRenameatxNP,
		darwinATFDCWD,
		uintptr(unsafe.Pointer(from)),
		darwinATFDCWD,
		uintptr(unsafe.Pointer(to)),
		darwinRenameSwap,
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}
