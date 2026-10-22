package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBaseRejectsAMissingFCMKeyFile(t *testing.T) {
	cfg := defaultConfig()
	cfg.Notify.FCM.ServiceAccountFile = "/nonexistent/sa.json"
	if err := cfg.ValidateBase(); err == nil || !strings.Contains(err.Error(), "notify.fcm.service_account_file") {
		t.Fatalf("ValidateBase() = %v", err)
	}
}

func TestLoadFCMServiceAccount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sa.json")
	if err := os.WriteFile(path, []byte(`{"project_id":"p","client_email":"e@p.iam","private_key":"k"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFCMServiceAccount(path); err == nil || !strings.Contains(err.Error(), "token_uri") {
		t.Fatalf("missing token_uri: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"project_id":"p","client_email":"e@p.iam","private_key":"k","token_uri":"https://oauth2.googleapis.com/token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	account, err := LoadFCMServiceAccount(path)
	if err != nil || account.ProjectID != "p" {
		t.Fatalf("account = %+v err=%v", account, err)
	}
}
