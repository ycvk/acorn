package cli

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/wire"
)

func runOwner(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("owner requires a subcommand: show or set")
	}
	switch args[0] {
	case "show":
		return runOwnerShow(ctx, args[1:])
	case "set":
		return runOwnerSet(ctx, args[1:])
	default:
		return fmt.Errorf("unknown owner subcommand %q", args[0])
	}
}

func runOwnerShow(ctx context.Context, args []string) error {
	fs := newFlagSet("owner show")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print what Acorn calls you as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		owner, err := container.Owner().Current(ctx)
		if err != nil {
			return err
		}
		if *jsonMode {
			return printJSON(owner)
		}
		if owner.Name == "" {
			fmt.Println("Acorn has no name for you; it calls you the owner.")
			return nil
		}
		fmt.Println(owner.Name)
		return nil
	})
}

func runOwnerSet(ctx context.Context, args []string) error {
	fs := newFlagSet("owner set")
	configPath := addConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf(`owner set requires the name Acorn should call you; "" leaves you unnamed`)
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		owner, err := container.Owner().SetName(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		if owner.Name == "" {
			fmt.Println("Acorn no longer has a name for you; it calls you the owner.")
			return nil
		}
		fmt.Printf("Acorn calls you %s.\n", owner.Name)
		return nil
	})
}
