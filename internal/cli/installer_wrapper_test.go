package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The installer's /usr/local/bin/acorn wrapper loads acorn.env for commands
// that open the storage, so they see the same secrets as the service. Only
// init, serve (run by systemd with the env file) and workstation (run in its
// container) go without it.
func TestInstallerWrapperLoadsTheServiceEnvForEveryStorageCommand(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install-release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^\t\t(doctor\|[a-z|]+)\)$`).FindSubmatch(script)
	if match == nil {
		t.Fatal("install-release.sh has no wrapper case listing the service commands")
	}
	wrapped := strings.Split(string(match[1]), "|")

	var commands []string
	for _, line := range strings.Split(usageText(), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "acorn" || fields[1] == "-" {
			continue
		}
		if name := fields[1]; !slices.Contains(commands, name) && !slices.Contains([]string{"init", "serve", "workstation"}, name) {
			commands = append(commands, name)
		}
	}
	slices.Sort(commands)
	slices.Sort(wrapped)
	if !slices.Equal(commands, wrapped) {
		t.Fatalf("wrapper loads the service env for %v; storage commands are %v", wrapped, commands)
	}
}
