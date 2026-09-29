package acli

import "os/exec"

var lookPath = exec.LookPath

func Available() bool {
	_, err := lookPath("acli")
	return err == nil
}
