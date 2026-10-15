package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/ycvk/acorn/internal/wire"
)

func runPersona(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("persona requires a subcommand: show or set")
	}
	switch args[0] {
	case "show":
		return runPersonaShow(ctx, args[1:])
	case "set":
		return runPersonaSet(ctx, args[1:])
	default:
		return fmt.Errorf("unknown persona subcommand %q", args[0])
	}
}

func runPersonaShow(ctx context.Context, args []string) error {
	fs := newFlagSet("persona show")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print the persona revision as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		persona, err := container.Persona().Current(ctx)
		if err != nil {
			return err
		}
		if *jsonMode {
			return printJSON(persona)
		}
		fmt.Println(persona.Content)
		return nil
	})
}

func runPersonaSet(ctx context.Context, args []string) error {
	fs := newFlagSet("persona set")
	configPath := addConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("persona set requires a file holding the new persona")
	}
	content, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("read persona: %w", err)
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		current, err := container.Persona().Current(ctx)
		if err != nil {
			return err
		}
		saved, err := container.Persona().Save(ctx, "", string(content), current.Revision)
		if err != nil {
			return err
		}
		fmt.Printf("Saved persona revision %d.\n", saved.Revision)
		return nil
	})
}
