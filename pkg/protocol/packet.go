package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	Version    = 1
	HeaderSize = 46
	MaxPayload = 65535
)

var Magic = [4]byte{'T', 'Y', 'X', 'N'}

type Type uint8

const (
	TypeHello Type = iota + 1
	TypeChallenge
	TypeAuth
	TypeAuthOK
	TypeData
	TypeKeepalive
	TypeControl
	TypeCommand
	TypeCommandResult
	TypeDisconnect
	TypeError
)

type Packet struct {
	Type          Type
	Flags         uint16
	NetworkID     uint32
	SessionID     uint64
	SourceID      uint64
	DestinationID uint64
	Sequence      uint64
	Payload       []byte
}

func (p Packet) MarshalBinary() ([]byte, error) {
	if len(p.Payload) > MaxPayload {
		return nil, errors.New("payload exceeds maximum")
	}
	b := make([]byte, HeaderSize+len(p.Payload))
	copy(b[:4], Magic[:])
	b[4] = Version
	b[5] = byte(p.Type)
	binary.BigEndian.PutUint16(b[6:8], p.Flags)
	binary.BigEndian.PutUint32(b[8:12], p.NetworkID)
	binary.BigEndian.PutUint64(b[12:20], p.SessionID)
	binary.BigEndian.PutUint64(b[20:28], p.SourceID)
	binary.BigEndian.PutUint64(b[28:36], p.DestinationID)
	binary.BigEndian.PutUint64(b[36:44], p.Sequence)
	binary.BigEndian.PutUint16(b[44:46], uint16(len(p.Payload)))
	copy(b[46:], p.Payload)
	return b, nil
}

func ParsePacket(b []byte) (Packet, error) {
	if len(b) < HeaderSize {
		return Packet{}, errors.New("short packet")
	}
	if string(b[:4]) != string(Magic[:]) {
		return Packet{}, errors.New("invalid magic")
	}
	if b[4] != Version {
		return Packet{}, fmt.Errorf("unsupported protocol version %d", b[4])
	}
	n := int(binary.BigEndian.Uint16(b[44:46]))
	if n > MaxPayload || len(b) != HeaderSize+n {
		return Packet{}, errors.New("invalid payload length")
	}
	return Packet{Type: Type(b[5]), Flags: binary.BigEndian.Uint16(b[6:8]), NetworkID: binary.BigEndian.Uint32(b[8:12]), SessionID: binary.BigEndian.Uint64(b[12:20]), SourceID: binary.BigEndian.Uint64(b[20:28]), DestinationID: binary.BigEndian.Uint64(b[28:36]), Sequence: binary.BigEndian.Uint64(b[36:44]), Payload: append([]byte(nil), b[46:]...)}, nil
}
