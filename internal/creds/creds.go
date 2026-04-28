package creds

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

const serviceName = "ai.prbe.agent-tap"

// StateDir returns the daemon's state directory: $PRBE_STATE_DIR or ~/.prbe.
func StateDir() (string, error) {
	if d := os.Getenv("PRBE_STATE_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".prbe"), nil
}

func keychainEnabled() bool {
	return os.Getenv("PRBE_DISABLE_KEYCHAIN") == ""
}

func Store(account, value string) error {
	if keychainEnabled() {
		if err := keyring.Set(serviceName, account, value); err == nil {
			return nil
		}
	}
	return storeFile(account, value)
}

func Load(account string) (string, error) {
	if keychainEnabled() {
		v, err := keyring.Get(serviceName, account)
		if err == nil {
			return v, nil
		}
		if !errors.Is(err, keyring.ErrNotFound) {
			// fall through to file
		}
	}
	return loadFile(account)
}

func Delete(account string) error {
	if keychainEnabled() {
		_ = keyring.Delete(serviceName, account)
	}
	return deleteFile(account)
}

func credentialsPath() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials"), nil
}

func readMap() (map[string]string, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	m := map[string]string{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func writeMap(m map[string]string) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func storeFile(account, value string) error {
	m, err := readMap()
	if err != nil {
		return err
	}
	m[account] = value
	return writeMap(m)
}

func loadFile(account string) (string, error) {
	m, err := readMap()
	if err != nil {
		return "", err
	}
	return m[account], nil
}

func deleteFile(account string) error {
	m, err := readMap()
	if err != nil {
		return err
	}
	delete(m, account)
	return writeMap(m)
}
