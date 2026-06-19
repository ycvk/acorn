package cli

import (
	"strings"
	"testing"
)

func TestRenderTokenIssueHumanAndJSON(t *testing.T) {
	out := tokenIssueOutput{DeviceID: "device_abc", Name: "cli", Platform: "backend", AccessToken: "acorn_dev_secret"}

	var human strings.Builder
	if err := renderTokenIssue(&human, out, false); err != nil {
		t.Fatalf("render human: %v", err)
	}
	for _, want := range []string{"device_abc", "acorn_dev_secret", "shown once", "acorn devices revoke device_abc"} {
		if !strings.Contains(human.String(), want) {
			t.Fatalf("human token output missing %q:\n%s", want, human.String())
		}
	}

	var jsonOut strings.Builder
	if err := renderTokenIssue(&jsonOut, out, true); err != nil {
		t.Fatalf("render json: %v", err)
	}
	for _, want := range []string{`"device_id": "device_abc"`, `"access_token": "acorn_dev_secret"`} {
		if !strings.Contains(jsonOut.String(), want) {
			t.Fatalf("json token output missing %q:\n%s", want, jsonOut.String())
		}
	}
}

func TestRenderDeviceList(t *testing.T) {
	var empty strings.Builder
	if err := renderDeviceList(&empty, nil, false); err != nil {
		t.Fatalf("render empty: %v", err)
	}
	if !strings.Contains(empty.String(), "No paired devices") {
		t.Fatalf("empty list output = %q", empty.String())
	}

	items := []deviceListItem{
		{DeviceID: "device_active", Name: "phone", Platform: "android", CreatedAt: "2026-06-19T00:00:00Z"},
		{DeviceID: "device_gone", Name: "cli", Platform: "backend", CreatedAt: "2026-06-19T00:00:00Z", RevokedAt: "2026-06-19T01:00:00Z"},
	}
	var human strings.Builder
	if err := renderDeviceList(&human, items, false); err != nil {
		t.Fatalf("render human: %v", err)
	}
	for _, want := range []string{"device_active", "phone", "active", "device_gone", "revoked 2026-06-19T01:00:00Z"} {
		if !strings.Contains(human.String(), want) {
			t.Fatalf("device list missing %q:\n%s", want, human.String())
		}
	}
}

func TestTokenAndDevicesDispatchErrors(t *testing.T) {
	if err := runToken(t.Context(), nil); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("token with no subcommand should usage-error, got: %v", err)
	}
	if err := runToken(t.Context(), []string{"bogus"}); err == nil || !strings.Contains(err.Error(), "unknown token subcommand") {
		t.Fatalf("token bogus should error, got: %v", err)
	}
	if err := runDevices(t.Context(), nil); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("devices with no subcommand should usage-error, got: %v", err)
	}
	if err := runDevices(t.Context(), []string{"bogus"}); err == nil || !strings.Contains(err.Error(), "unknown devices subcommand") {
		t.Fatalf("devices bogus should error, got: %v", err)
	}
}

func TestUsageIncludesTokenAndDevices(t *testing.T) {
	usage := usageText()
	for _, want := range []string{"acorn token issue", "acorn devices list", "acorn devices revoke"} {
		if !strings.Contains(usage, want) {
			t.Fatalf("usage missing %q:\n%s", want, usage)
		}
	}
}
