package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mdp/qrterminal/v3"

	"github.com/ycvk/acorn/internal/app"
)

type pairCommandOutput struct {
	PairingCode string `json:"pairing_code"`
	ExpiresAt   string `json:"expires_at"`
	ServerURL   string `json:"server_url,omitempty"`
}

func runPair(ctx context.Context, args []string) error {
	fs := newFlagSet("pair")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print pairing code as JSON")
	qrMode := fs.Bool("qr", false, "print a terminal QR code containing the pairing payload")
	ttl := fs.Duration("ttl", 10*time.Minute, "pairing code time to live, for example 10m")
	serverURL := fs.String("server-url", "", "self-hosted server URL to include in pairing output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("pair does not accept positional arguments")
	}
	if *ttl <= 0 {
		return fmt.Errorf("pair ttl must be positive")
	}
	if *jsonMode && *qrMode {
		return fmt.Errorf("pair --json and --qr cannot be used together")
	}
	if *qrMode && strings.TrimSpace(*serverURL) == "" {
		return fmt.Errorf("pair --qr requires --server-url")
	}
	return withContainer(ctx, *configPath, func(container *app.Container) error {
		code, err := container.DeviceAuth().CreatePairingCode(ctx, *ttl)
		if err != nil {
			return err
		}
		return printPairOutput(os.Stdout, pairCommandOutput{
			PairingCode: code.Code,
			ExpiresAt:   code.ExpiresAt.UTC().Format(time.RFC3339Nano),
			ServerURL:   strings.TrimSpace(*serverURL),
		}, *jsonMode, *qrMode)
	})
}

func printPairOutput(w io.Writer, output pairCommandOutput, jsonMode bool, qrMode bool) error {
	if jsonMode {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)
	}
	if qrMode {
		payload, err := pairPayloadJSON(output)
		if err != nil {
			return err
		}
		fmt.Fprintln(w, "Pairing QR payload:")
		qrterminal.GenerateWithConfig(payload, qrterminal.Config{
			Level:      qrterminal.M,
			Writer:     w,
			HalfBlocks: true,
			QuietZone:  1,
		})
		fmt.Fprintln(w)
	}
	if output.ServerURL != "" {
		fmt.Fprintf(w, "Server URL: %s\n", output.ServerURL)
	}
	fmt.Fprintf(w, "Pairing code: %s\n", output.PairingCode)
	fmt.Fprintf(w, "Expires at: %s\n", output.ExpiresAt)
	return nil
}

func pairPayloadJSON(output pairCommandOutput) (string, error) {
	if strings.TrimSpace(output.ServerURL) == "" {
		return "", fmt.Errorf("pairing QR payload requires server_url")
	}
	payload := pairCommandOutput{
		PairingCode: strings.TrimSpace(output.PairingCode),
		ExpiresAt:   strings.TrimSpace(output.ExpiresAt),
		ServerURL:   strings.TrimSpace(output.ServerURL),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode pairing payload: %w", err)
	}
	return string(encoded), nil
}
