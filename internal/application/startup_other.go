//go:build !windows && !linux && !darwin

package application

import "errors"

func StartupAvailable() (bool, string) {
	return false, "startup integration is not supported on this platform"
}

func StartupEnabled(StartupSpec) (bool, error) {
	return false, errors.New("startup integration is not supported on this platform")
}
func SetStartup(StartupSpec, bool) error {
	return errors.New("startup integration is not supported on this platform")
}
