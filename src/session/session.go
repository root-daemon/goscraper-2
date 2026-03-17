package session

import (
	"errors"
	"fmt"
	"goscraper/src/handlers"
	"goscraper/src/helpers/databases"
	"goscraper/src/utils"
	"log"
	"strings"
)

type Manager struct {
	db *databases.DatabaseHelper
}

type RetryResult struct {
	Data       interface{}
	NewCookies string
}

func NewManager() (*Manager, error) {
	db, err := databases.NewDatabaseHelper()
	if err != nil {
		return nil, err
	}
	return &Manager{db: db}, nil
}

func (sm *Manager) RefreshSession(oldToken string) (string, error) {
	creds, err := sm.db.GetCredentialsByToken(oldToken)
	if err != nil {
		return "", fmt.Errorf("failed to fetch credentials: %w", err)
	}
	if creds == nil {
		return "", errors.New("no stored credentials found")
	}

	lf := &handlers.LoginFetcher{}
	loginResp, err := lf.Login(creds.Account, creds.Password, nil, nil)
	if err != nil {
		return "", fmt.Errorf("auto-relogin failed: %w", err)
	}

	if loginResp.Captcha != nil {
		return "", errors.New("auto-relogin blocked by CAPTCHA")
	}

	if !loginResp.Authenticated || loginResp.Cookies == "" {
		msg := "unknown"
		if loginResp.Message != nil {
			msg = fmt.Sprintf("%v", loginResp.Message)
		}
		return "", fmt.Errorf("auto-relogin failed: %s", msg)
	}

	newToken := utils.Encode(loginResp.Cookies)
	if err := sm.db.UpdateSession(creds.RegNumber, newToken, loginResp.Cookies); err != nil {
		log.Printf("session: failed to persist refreshed session: %v", err)
	}

	return loginResp.Cookies, nil
}

func isSessionExpiredError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "invalid response format") ||
		strings.Contains(msg, "invalid token format")
}

func WithAutoRetry(cookie string, fn func(string) (interface{}, error)) (*RetryResult, error) {
	result, err := fn(cookie)
	if err == nil {
		return &RetryResult{Data: result}, nil
	}

	if !isSessionExpiredError(err) {
		return nil, err
	}

	log.Printf("session: detected expired session, attempting auto-relogin")

	sm, smErr := NewManager()
	if smErr != nil {
		return nil, err
	}

	oldToken := utils.Encode(cookie)
	newCookies, refreshErr := sm.RefreshSession(oldToken)
	if refreshErr != nil {
		log.Printf("session: auto-relogin failed: %v", refreshErr)
		return nil, err
	}

	log.Printf("session: auto-relogin succeeded, retrying request")

	retryResult, retryErr := fn(newCookies)
	if retryErr != nil {
		return nil, retryErr
	}

	return &RetryResult{Data: retryResult, NewCookies: newCookies}, nil
}
