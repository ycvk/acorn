package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// NotifyConfig configures owner push notifications.
type NotifyConfig struct {
	FCM FCMConfig `yaml:"fcm"`
}

// FCMConfig points at a Firebase service account key. Empty disables push.
type FCMConfig struct {
	ServiceAccountFile string `yaml:"service_account_file"`
}

// FCMServiceAccount is the part of a Firebase service account key that FCM
// HTTP v1 needs.
type FCMServiceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// LoadFCMServiceAccount reads and checks a Firebase service account key.
func LoadFCMServiceAccount(path string) (FCMServiceAccount, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FCMServiceAccount{}, fmt.Errorf("notify.fcm.service_account_file: %w", err)
	}
	var account FCMServiceAccount
	if err := json.Unmarshal(data, &account); err != nil {
		return FCMServiceAccount{}, fmt.Errorf("notify.fcm.service_account_file %s: %w", path, err)
	}
	for field, value := range map[string]string{
		"project_id": account.ProjectID, "client_email": account.ClientEmail,
		"private_key": account.PrivateKey, "token_uri": account.TokenURI,
	} {
		if strings.TrimSpace(value) == "" {
			return FCMServiceAccount{}, fmt.Errorf("notify.fcm.service_account_file %s: %s is missing", path, field)
		}
	}
	return account, nil
}

func (c *Config) validateNotify() error {
	if file := c.Notify.FCM.ServiceAccountFile; file != "" {
		if _, err := LoadFCMServiceAccount(file); err != nil {
			return err
		}
	}
	return nil
}
