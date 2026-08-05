package commands

import "errors"

func systemCommand(commandType string) (string, []string, error) {
	switch commandType {
	case "system.restart":
		return "systemctl", []string{"reboot"}, nil
	case "system.shutdown":
		return "systemctl", []string{"poweroff"}, nil
	default:
		return "", nil, errors.New("command has no Linux system action")
	}
}
