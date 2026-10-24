package acli

import (
	"errors"
	"testing"
)

func TestAvailable_Found(t *testing.T) {
	orig := lookPath
	defer func() { lookPath = orig }()
	lookPath = func(file string) (string, error) {
		return "/usr/local/bin/" + file, nil
	}

	if !Available() {
		t.Error("Available() = false, want true when lookup succeeds")
	}
}

func TestAvailable_NotFound(t *testing.T) {
	orig := lookPath
	defer func() { lookPath = orig }()
	lookPath = func(file string) (string, error) {
		return "", errors.New("exec: \"acli\": executable file not found in $PATH")
	}

	if Available() {
		t.Error("Available() = true, want false when lookup fails")
	}
}
