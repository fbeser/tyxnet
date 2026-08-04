package application

import (
	"crypto/rand"
	"encoding/hex"
	"os"
)

type StartupSpec struct {
	ID               string
	DisplayName      string
	Executable       string
	WorkingDirectory string
	Arguments        []string
	Companion        string
	CompanionArgs    []string
	TrayToken        string
}

func TrayToken() string {
	if value := os.Getenv("TYXNET_TRAY_TOKEN"); value != "" {
		return value
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
