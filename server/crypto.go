package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

func loadSecret(dataDir string) []byte {
	path := filepath.Join(dataDir, "secret.key")
	if raw, err := os.ReadFile(path); err == nil && len(raw) == 32 {
		return raw
	}
	key := make([]byte, 32)
	_, err := rand.Read(key)
	must(err)
	must(os.WriteFile(path, key, 0600))
	return key
}

func seal(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	data := gcm.Seal(nonce, nonce, []byte(plaintext), []byte("today-random-movie"))
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func openSealed(key []byte, encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", fmt.Errorf("加密数据损坏")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte("today-random-movie"))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
