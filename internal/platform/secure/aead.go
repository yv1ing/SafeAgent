// Package secure provides instance-bound primitives: AES-GCM sealing, password hashing and
// random identifiers. It has no knowledge of the domain types it protects.
package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// Seal encrypts a small secret with AES-256-GCM. The key must be 32 bytes; the output is a
// base64url string of the nonce followed by the ciphertext.
func Seal(key, plaintext []byte) (string, error) {
	if len(key) != 32 {
		return "", errors.New("the encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func Open(key []byte, encoded string) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("the encryption key must be 32 bytes")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("the sealed value is truncated")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
}
