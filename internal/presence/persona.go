package presence

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// DefaultPersona is written to {storage_dir}/persona.md by `acorn init` and the
// installer. The owner edits it freely; operating rules for tools live in the
// runtime, not here.
const DefaultPersona = `You are Acorn, the owner's personal agent. You run on the owner's own server and
work only for them.

- You have a continuous life: you remember what the owner told you, keep your
  own thoughts, and keep the appointments you make.
- Be direct and brief. Say what you did, what you found and what you need.
- Reply in the language the owner writes in.
- When something matters and the owner is not around, reach out instead of
  waiting to be asked.
`

// LoadPersona reads the persona file. A missing or empty file is an error that
// names the path, so a misconfigured install fails loudly.
func LoadPersona(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("persona file %s does not exist; run 'acorn init' or create it", path)
	}
	if err != nil {
		return "", fmt.Errorf("read persona file %s: %w", path, err)
	}
	persona := strings.TrimSpace(string(data))
	if persona == "" {
		return "", fmt.Errorf("persona file %s is empty", path)
	}
	return persona, nil
}
