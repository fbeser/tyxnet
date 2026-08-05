package protocol

import (
	"bytes"
	"testing"
)

func TestCommandResultSigningPayloadIsDomainSeparatedAndBound(t *testing.T) {
	nonce := bytes.Repeat([]byte{1}, 32)
	first, err := CommandResultSigningPayload(nonce, "dev_1", "cmd_1", "accepted", "", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := CommandResultSigningPayload(nonce, "dev_1", "cmd_1", "failed", "", "denied")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, nonce) || bytes.Equal(first, second) {
		t.Fatal("command result proof is not bound to its domain and fields")
	}
	if _, err = CommandResultSigningPayload(nil, "dev_1", "cmd_1", "accepted", "", ""); err == nil {
		t.Fatal("invalid nonce was accepted")
	}
}
