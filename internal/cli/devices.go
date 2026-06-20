package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/app"
)

type deviceListItem struct {
	DeviceID   string `json:"device_id"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at,omitempty"`
	RevokedAt  string `json:"revoked_at,omitempty"`
}

// runDevices dispatches `acorn devices <subcommand>`. Both subcommands run against
// the local SQLite via the container (no bearer token needed) so a single owner can
// recover or rotate access from the box even after losing every token.
func runDevices(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: acorn devices list|revoke ...")
	}
	switch args[0] {
	case "list":
		return runDevicesList(ctx, args[1:])
	case "revoke":
		return runDevicesRevoke(ctx, args[1:])
	default:
		return fmt.Errorf("unknown devices subcommand %q (want: list, revoke)", args[0])
	}
}

func runDevicesList(ctx context.Context, args []string) error {
	fs := newFlagSet("devices list")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print devices as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withContainer(ctx, *configPath, func(container *app.Container) error {
		devices, err := container.DeviceAuth().ListDevices(ctx)
		if err != nil {
			return err
		}
		items := make([]deviceListItem, 0, len(devices))
		for _, d := range devices {
			item := deviceListItem{
				DeviceID:  d.DeviceID,
				Name:      d.Name,
				Platform:  d.Platform,
				CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339),
			}
			if !d.LastSeenAt.IsZero() {
				item.LastSeenAt = d.LastSeenAt.UTC().Format(time.RFC3339)
			}
			if d.RevokedAt != nil {
				item.RevokedAt = d.RevokedAt.UTC().Format(time.RFC3339)
			}
			items = append(items, item)
		}
		return renderDeviceList(os.Stdout, items, *jsonMode)
	})
}

func renderDeviceList(w io.Writer, items []deviceListItem, jsonMode bool) error {
	if jsonMode {
		body, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return fmt.Errorf("encode devices: %w", err)
		}
		_, err = fmt.Fprintln(w, string(body))
		return err
	}
	if len(items) == 0 {
		_, err := fmt.Fprintln(w, "No paired devices. Pair a phone with 'acorn pair --qr', or mint a token with 'acorn token issue'.")
		return err
	}
	for _, it := range items {
		status := "active"
		if it.RevokedAt != "" {
			status = "revoked " + it.RevokedAt
		}
		fmt.Fprintf(w, "%s  %-12s  [%s]  created=%s  %s\n", it.DeviceID, it.Name, it.Platform, it.CreatedAt, status)
	}
	return nil
}

func runDevicesRevoke(ctx context.Context, args []string) error {
	fs := newFlagSet("devices revoke")
	configPath := addConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	deviceID := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if deviceID == "" {
		return fmt.Errorf("usage: acorn devices revoke [-c path] DEVICE_ID")
	}
	return withContainer(ctx, *configPath, func(container *app.Container) error {
		if err := container.DeviceAuth().RevokeDevice(ctx, deviceID); err != nil {
			return err
		}
		fmt.Printf("Revoked device %s. Its bearer token no longer authenticates against /v1.\n", deviceID)
		return nil
	})
}
