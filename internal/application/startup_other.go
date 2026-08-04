//go:build !windows && !linux && !darwin

package application

import "errors"

func StartupEnabled(StartupSpec) (bool, error) {
	return false, errors.New("startup integration is not supported on this platform")
}
func SetStartup(StartupSpec, bool) error {
	return errors.New("startup integration is not supported on this platform")
}
