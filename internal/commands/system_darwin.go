package commands

import "errors"

func systemCommand(commandType string) (string, []string, error) {
	switch commandType {
	case "system.restart":
		return "/sbin/shutdown", []string{"-r", "now"}, nil
	case "system.shutdown":
		return "/sbin/shutdown", []string{"-h", "now"}, nil
	default:
		return "", nil, errors.New("command has no macOS system action")
	}
}
