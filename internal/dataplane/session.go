package dataplane

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"time"

	tyxcrypto "github.com/fbeser/tyxnet/internal/crypto"
	"github.com/fbeser/tyxnet/pkg/protocol"
)

const (
	ProtocolVersion = 1
	sessionLifetime = 15 * time.Minute
	keepalivePeriod = 15 * time.Second
)

type Bootstrap struct {
	ProtocolVersion int       `json:"protocol_version"`
	SessionID       string    `json:"session_id"`
	Secret          string    `json:"secret"`
	Port            int       `json:"port"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type sessionKeys struct {
	clientToServer []byte
	serverToClient []byte
}

func newBootstrap(port int) (Bootstrap, sessionKeys, uint64, error) {
	secret := make([]byte, tyxcrypto.DataKeySize)
	if _, err := rand.Read(secret); err != nil {
		return Bootstrap{}, sessionKeys{}, 0, err
	}
	var sessionID uint64
	for sessionID == 0 {
		if err := binary.Read(rand.Reader, binary.BigEndian, &sessionID); err != nil {
			return Bootstrap{}, sessionKeys{}, 0, err
		}
	}
	keys, err := deriveKeys(secret, sessionID)
	if err != nil {
		return Bootstrap{}, sessionKeys{}, 0, err
	}
	bootstrap := Bootstrap{ProtocolVersion: ProtocolVersion, SessionID: strconv.FormatUint(sessionID, 10), Secret: base64.RawStdEncoding.EncodeToString(secret), Port: port, ExpiresAt: time.Now().UTC().Add(sessionLifetime)}
	return bootstrap, keys, sessionID, nil
}

func parseBootstrap(bootstrap Bootstrap) (sessionKeys, uint64, error) {
	if bootstrap.ProtocolVersion != ProtocolVersion || bootstrap.Port < 1 || bootstrap.Port > 65535 || time.Now().After(bootstrap.ExpiresAt) {
		return sessionKeys{}, 0, errors.New("invalid or expired data-plane bootstrap")
	}
	sessionID, err := strconv.ParseUint(bootstrap.SessionID, 10, 64)
	if err != nil || sessionID == 0 {
		return sessionKeys{}, 0, errors.New("invalid data-plane session ID")
	}
	secret, err := base64.RawStdEncoding.DecodeString(bootstrap.Secret)
	if err != nil {
		return sessionKeys{}, 0, errors.New("invalid data-plane secret encoding")
	}
	keys, err := deriveKeys(secret, sessionID)
	return keys, sessionID, err
}

func deriveKeys(secret []byte, sessionID uint64) (sessionKeys, error) {
	clientToServer, err := tyxcrypto.DeriveDirectionalKey(secret, sessionID, "client-to-server")
	if err != nil {
		return sessionKeys{}, err
	}
	serverToClient, err := tyxcrypto.DeriveDirectionalKey(secret, sessionID, "server-to-client")
	if err != nil {
		return sessionKeys{}, err
	}
	return sessionKeys{clientToServer: clientToServer, serverToClient: serverToClient}, nil
}

func sealPacket(cipher *tyxcrypto.Cipher, packet protocol.Packet, plaintext []byte) ([]byte, error) {
	packet.Payload = make([]byte, len(plaintext)+16)
	encoded, err := packet.MarshalBinary()
	if err != nil {
		return nil, err
	}
	ciphertext := cipher.Seal(packet.SessionID, packet.Sequence, plaintext, encoded[:protocol.HeaderSize])
	copy(encoded[protocol.HeaderSize:], ciphertext)
	return encoded, nil
}

func openPacket(cipher *tyxcrypto.Cipher, encoded []byte, packet protocol.Packet) ([]byte, error) {
	if len(encoded) != protocol.HeaderSize+len(packet.Payload) {
		return nil, fmt.Errorf("invalid encrypted packet length")
	}
	return cipher.Open(packet.SessionID, packet.Sequence, packet.Payload, encoded[:protocol.HeaderSize])
}
