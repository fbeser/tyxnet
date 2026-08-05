package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"time"
)

const commandResultDomain = "tyxnet-command-result-v1"

type ControlCommand struct {
	ProtocolVersion int       `json:"protocol_version"`
	ID              string    `json:"id"`
	Type            string    `json:"type"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type CommandResult struct {
	DeviceID  string `json:"device_id"`
	Status    string `json:"status"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	Signature string `json:"signature"`
}

func CommandResultSigningPayload(nonce []byte, deviceID, commandID, status, result, commandError string) ([]byte, error) {
	if len(nonce) != 32 || deviceID == "" || commandID == "" {
		return nil, errors.New("invalid command result proof fields")
	}
	var payload bytes.Buffer
	payload.WriteString(commandResultDomain)
	payload.WriteByte(0)
	payload.Write(nonce)
	for _, value := range []string{deviceID, commandID, status, result, commandError} {
		if len(value) > 65535 {
			return nil, errors.New("command result proof field is too large")
		}
		_ = binary.Write(&payload, binary.BigEndian, uint16(len(value)))
		payload.WriteString(value)
	}
	return payload.Bytes(), nil
}
