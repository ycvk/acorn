package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/wire"
)

type tokenIssueOutput struct {
	DeviceID    string `json:"device_id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	AccessToken string `json:"access_token"`
}

// runToken dispatches `acorn token <subcommand>`.
func runToken(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: acorn token issue [-c path] [--json] [--name name] [--ttl duration]")
	}
	switch args[0] {
	case "issue":
		return runTokenIssue(ctx, args[1:])
	default:
		return fmt.Errorf("unknown token subcommand %q (want: issue)", args[0])
	}
}

// runTokenIssue mints a device bearer token in one step (pairing-code create +
// immediate pair) so a phone-less owner — or a script driving their own backend —
// can obtain a /v1 token from the box without the mobile pairing dance. Running
// against the local SQLite via the container is legitimate: root-on-the-box is the
// single owner's out-of-band authority.
func runTokenIssue(ctx context.Context, args []string) error {
	fs := newFlagSet("token issue")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print the issued token as JSON")
	name := fs.String("name", "cli", "device name to attach to the token")
	ttl := fs.Duration("ttl", 10*time.Minute, "pairing window the token is minted within")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *ttl <= 0 {
		return fmt.Errorf("token --ttl must be positive")
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		code, err := container.DeviceAuth().CreatePairingCode(ctx, *ttl)
		if err != nil {
			return err
		}
		result, err := container.DeviceAuth().PairDevice(ctx, api.PairDeviceInput{
			PairingCode: code.Code,
			DeviceName:  strings.TrimSpace(*name),
			Platform:    "backend",
		})
		if err != nil {
			return err
		}
		return renderTokenIssue(os.Stdout, tokenIssueOutput{
			DeviceID:    result.Device.DeviceID,
			Name:        result.Device.Name,
			Platform:    result.Device.Platform,
			AccessToken: result.AccessToken,
		}, *jsonMode)
	})
}

func renderTokenIssue(w io.Writer, out tokenIssueOutput, jsonMode bool) error {
	if jsonMode {
		body, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return fmt.Errorf("encode token: %w", err)
		}
		_, err = fmt.Fprintln(w, string(body))
		return err
	}
	fmt.Fprintf(w, "Device paired: %s (%s)\n", out.DeviceID, out.Name)
	fmt.Fprintf(w, "Bearer token (shown once — store it now):\n%s\n", out.AccessToken)
	fmt.Fprintf(w, "Use it as 'Authorization: Bearer <token>' against /v1. Revoke with 'acorn devices revoke %s'.\n", out.DeviceID)
	return nil
}
