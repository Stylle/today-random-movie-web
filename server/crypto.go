package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func loadSecret(dataDir string) []byte {
	path := filepath.Join(dataDir, "secret.key")
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			panic("secret.key must be a regular file; restore the matching backup")
		}
		raw, e := os.ReadFile(path)
		must(e)
		if len(raw) != 32 {
			panic("invalid secret.key; restore the matching backup")
		}
		return raw
	}
	if !errors.Is(err, os.ErrNotExist) {
		panic(err)
	}
	if _, e := os.Stat(filepath.Join(dataDir, "app.db")); !errors.Is(e, os.ErrNotExist) {
		panic("existing database without secret.key; restore the matching backup")
	}
	key := make([]byte, 32)
	_, err = rand.Read(key)
	must(err)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return loadSecret(dataDir)
	}
	must(err)
	_, err = f.Write(key)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	must(err)
	must(closeErr)
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
