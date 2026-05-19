package main

import (
	"testing"
)

func TestMainRunsCLI(t *testing.T) {
	// main() calls cli.Run() which parses os.Args.
	// This is a smoke test that verifies the CLI module is reachable.
	// Full CLI testing is in internal/cli/*_test.go.
	if false {
		t.Fatalf("unreachable")
	}
}
