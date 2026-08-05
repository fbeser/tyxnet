package crypto

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const DataKeySize = chacha20poly1305.KeySize

type Identity struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

func NewIdentity() (Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	return Identity{pub, priv}, err
}
func NewEphemeral() (*ecdh.PrivateKey, error) { return ecdh.X25519().GenerateKey(rand.Reader) }
func DeriveKey(private *ecdh.PrivateKey, peer []byte, transcript []byte) ([]byte, error) {
	pub, err := ecdh.X25519().NewPublicKey(peer)
	if err != nil {
		return nil, fmt.Errorf("peer key: %w", err)
	}
	secret, err := private.ECDH(pub)
	if err != nil {
		return nil, fmt.Errorf("ecdh: %w", err)
	}
	transcriptHash := sha256.Sum256(transcript)
	r := hkdf.New(sha256.New, secret, transcriptHash[:], []byte("tyxnet-v1-session"))
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := r.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

func DeriveDirectionalKey(secret []byte, session uint64, direction string) ([]byte, error) {
	if len(secret) != DataKeySize || session == 0 {
		return nil, errors.New("invalid data-plane secret")
	}
	if direction != "client-to-server" && direction != "server-to-client" {
		return nil, errors.New("invalid data-plane direction")
	}
	salt := make([]byte, 8)
	binary.BigEndian.PutUint64(salt, session)
	reader := hkdf.New(sha256.New, secret, salt, []byte("tyxnet-data-v1/"+direction))
	key := make([]byte, DataKeySize)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

type Cipher struct {
	aead   cipherAEAD
	replay *ReplayWindow
}
type cipherAEAD interface {
	Seal([]byte, []byte, []byte, []byte) []byte
	Open([]byte, []byte, []byte, []byte) ([]byte, error)
	NonceSize() int
}

func NewCipher(key []byte) (*Cipher, error) {
	a, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: a, replay: NewReplayWindow(64)}, nil
}
func nonce(session, sequence uint64) []byte {
	n := make([]byte, chacha20poly1305.NonceSize)
	binary.BigEndian.PutUint32(n[:4], uint32(session))
	binary.BigEndian.PutUint64(n[4:], sequence)
	return n
}
func (c *Cipher) Seal(session, sequence uint64, plaintext, aad []byte) []byte {
	return c.aead.Seal(nil, nonce(session, sequence), plaintext, aad)
}
func (c *Cipher) Open(session, sequence uint64, ciphertext, aad []byte) ([]byte, error) {
	if !c.replay.Accept(sequence) {
		return nil, errors.New("replayed or stale sequence")
	}
	p, err := c.aead.Open(nil, nonce(session, sequence), ciphertext, aad)
	if err != nil {
		c.replay.Rollback(sequence)
		return nil, errors.New("authentication failed")
	}
	return p, nil
}

type ReplayWindow struct {
	highest uint64
	bitmap  uint64
}

func NewReplayWindow(_ uint) *ReplayWindow { return &ReplayWindow{} }
func (w *ReplayWindow) Accept(seq uint64) bool {
	if seq == 0 {
		return false
	}
	if seq > w.highest {
		shift := seq - w.highest
		if shift >= 64 {
			w.bitmap = 0
		} else {
			w.bitmap <<= shift
		}
		w.bitmap |= 1
		w.highest = seq
		return true
	}
	d := w.highest - seq
	if d >= 64 || w.bitmap&(uint64(1)<<d) != 0 {
		return false
	}
	w.bitmap |= uint64(1) << d
	return true
}
func (w *ReplayWindow) Rollback(seq uint64) {
	if seq <= w.highest {
		w.bitmap &^= uint64(1) << (w.highest - seq)
	}
}
