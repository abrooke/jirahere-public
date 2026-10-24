package profile

import (
	"errors"
	"fmt"
)

const MaxLen = 64

func Validate(name string) error {
	if name == "" {
		return errors.New("profile name must not be empty")
	}
	if len(name) > MaxLen {
		return fmt.Errorf("profile name is %d bytes; the maximum is %d", len(name), MaxLen)
	}
	if name == ".." {
		return errors.New(`profile name must not be ".."`)
	}
	if c := name[0]; c == '.' || c == '-' || c == '_' {
		return fmt.Errorf("profile name must not start with %q", c)
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '/' || c == '\\':
			return errors.New("profile name must not contain a path separator (/ or \\)")
		case c < 0x20 || c == 0x7f:
			return errors.New("profile name must not contain NUL or control characters")
		case c == ' ':
			return errors.New("profile name must not contain whitespace")
		case c >= 0x80:
			return errors.New("profile name must be ASCII")
		case !allowed(c):
			return errors.New("profile name may contain only letters, digits, '.', '_' and '-'")
		}
	}
	return nil
}

func allowed(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return c == '.' || c == '_' || c == '-'
}
