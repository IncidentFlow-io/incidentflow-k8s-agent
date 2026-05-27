package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type TokenStore struct {
	path string
}

func NewTokenStore(path string) TokenStore {
	return TokenStore{path: path}
}

func (s TokenStore) Load() (string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (s TokenStore) Save(token string) error {
	if token == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.path, []byte(token+"\n"), 0o600)
}
