package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

const (
	serviceName = "devcraft-cli"
	accountName = "default-user"

	configDirName   = "devcraft"
	credentialsFile = "credentials.json"
)

// ErrNotLoggedIn is returned when no stored session exists.
var ErrNotLoggedIn = errors.New("not logged in")

// Session is the login state persisted after a successful login.
type Session struct {
	IDToken          string `json:"id_token"`
	AuthReferencePid string `json:"auth_reference_pid"`
	Email            string `json:"email"`
	Name             string `json:"name"`
}

type AuthInterface interface {
	SaveLogin(session Session) error
	LoadLogin() (*Session, error)
	DeleteLogin() error
}

type authHandler struct{}

func NewAuthHandler() AuthInterface {
	return &authHandler{}
}

// SaveLogin stores the session in the OS keychain, falling back to a
// 0600 file in the user config directory when no keychain is available.
func (a *authHandler) SaveLogin(session Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}

	if err := keyring.Set(serviceName, accountName, string(data)); err == nil {
		// Remove any stale plaintext fallback from an earlier login.
		if path, pErr := credentialsPath(); pErr == nil {
			_ = os.Remove(path)
		}
		return nil
	}

	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	fmt.Fprintf(os.Stderr, "warning: OS keychain unavailable, credentials stored in plaintext at %s\n", path)
	return nil
}

// LoadLogin returns the stored session, or ErrNotLoggedIn if none exists.
func (a *authHandler) LoadLogin() (*Session, error) {
	var raw []byte

	secret, err := keyring.Get(serviceName, accountName)
	if err == nil {
		raw = []byte(secret)
	} else {
		path, pErr := credentialsPath()
		if pErr != nil {
			return nil, pErr
		}
		raw, err = os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotLoggedIn
		}
		if err != nil {
			return nil, fmt.Errorf("read credentials: %w", err)
		}
	}

	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	return &s, nil
}

// DeleteLogin removes the stored session from both the keychain and the
// fallback file.
func (a *authHandler) DeleteLogin() error {
	if err := keyring.Delete(serviceName, accountName); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete keychain entry: %w", err)
	}
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete credentials file: %w", err)
	}
	return nil
}

func credentialsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config dir: %w", err)
	}
	return filepath.Join(dir, configDirName, credentialsFile), nil
}
