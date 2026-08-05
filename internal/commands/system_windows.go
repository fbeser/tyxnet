package commands

import "errors"

func systemCommand(commandType string) (string, []string, error) {
	switch commandType {
	case "system.restart":
		return "shutdown.exe", []string{"/r", "/t", "5", "/d", "p:0:0", "/c", "TyxNet administrator requested a restart"}, nil
	case "system.shutdown":
		return "shutdown.exe", []string{"/s", "/t", "5", "/d", "p:0:0", "/c", "TyxNet administrator requested a shutdown"}, nil
	default:
		return "", nil, errors.New("command has no Windows system action")
	}
}
