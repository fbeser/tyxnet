//go:build !linux && !windows && !darwin

package commands

import "errors"

func systemCommand(string) (string, []string, error) {
	return "", nil, errors.New("system command is not implemented on this platform")
}
