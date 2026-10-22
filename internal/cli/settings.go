package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/ycvk/acorn/internal/settings"
	"github.com/ycvk/acorn/internal/wire"
)

func runSettings(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("settings requires a subcommand: show, set or reset")
	}
	switch args[0] {
	case "show":
		return runSettingsShow(ctx, args[1:])
	case "set":
		return runSettingsSet(ctx, args[1:])
	case "reset":
		return runSettingsReset(ctx, args[1:])
	default:
		return fmt.Errorf("unknown settings subcommand %q", args[0])
	}
}

func runSettingsShow(ctx context.Context, args []string) error {
	fs := newFlagSet("settings show")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print the settings as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		current, err := container.Settings().Current(ctx)
		if err != nil {
			return err
		}
		if *jsonMode {
			return printJSON(struct {
				Values     settings.Values `json:"values"`
				Defaults   settings.Values `json:"defaults"`
				Customized []string        `json:"customized"`
				Usage      settings.Usage  `json:"usage"`
			}{current.Values, current.Defaults, current.Customized, current.Usage})
		}
		text, err := renderSettings(current)
		if err != nil {
			return err
		}
		fmt.Println(text)
		return nil
	})
}

func runSettingsSet(ctx context.Context, args []string) error {
	fs := newFlagSet("settings set")
	configPath := addConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("settings set requires a key and its values; the keys are %s", strings.Join(settings.Keys(), ", "))
	}
	key := fs.Arg(0)
	value, err := settings.Parse(key, fs.Args()[1:])
	if err != nil {
		return err
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		current, err := container.Settings().Save(ctx, settings.Change{Set: map[string]json.RawMessage{key: value}}, "")
		if err != nil {
			return err
		}
		return printSetting(current, key)
	})
}

func runSettingsReset(ctx context.Context, args []string) error {
	fs := newFlagSet("settings reset")
	configPath := addConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("settings reset requires the keys to return to their defaults")
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		current, err := container.Settings().Save(ctx, settings.Change{Reset: fs.Args()}, "")
		if err != nil {
			return err
		}
		for _, key := range fs.Args() {
			if err := printSetting(current, key); err != nil {
				return err
			}
		}
		return nil
	})
}

func printSetting(current settings.Current, key string) error {
	value, err := settingText(current.Values, key)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %s\n", key, value)
	return nil
}

// renderSettings lists every setting with its value, whether it is the
// default, and what the budgets used today.
func renderSettings(current settings.Current) (string, error) {
	used := map[string]int{
		settings.KeyWakeDailyLimit:    current.Usage.Wakes,
		settings.KeyWakeDailyTokens:   current.Usage.WakeTokens,
		settings.KeyMemoryDailyTokens: current.Usage.MemoryTokens,
		settings.KeyNewsDailyTokens:   current.Usage.NewsTokens,
	}
	keys := settings.Keys()
	width := 0
	for _, key := range keys {
		width = max(width, len(key))
	}
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		value, err := settingText(current.Values, key)
		if err != nil {
			return "", err
		}
		if !slices.Contains(current.Customized, key) {
			value += " (default)"
		}
		if n, ok := used[key]; ok {
			value += fmt.Sprintf("; %d used on %s", n, current.Usage.Day)
		}
		lines = append(lines, fmt.Sprintf("%-*s  %s", width, key, value))
	}
	return strings.Join(lines, "\n"), nil
}

// settingText shows one setting: off for an empty time or window, none for an
// empty list.
func settingText(values settings.Values, key string) (string, error) {
	body, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		return "", err
	}
	value, ok := fields[settings.NameForKey(key)]
	if !ok {
		return "", fmt.Errorf("%q is not a setting; the keys are %s", key, strings.Join(settings.Keys(), ", "))
	}
	switch v := value.(type) {
	case string:
		if v == "" {
			return "off", nil
		}
		return v, nil
	case []any:
		if len(v) == 0 {
			return "none", nil
		}
		items := make([]string, len(v))
		for i, item := range v {
			items[i] = fmt.Sprint(item)
		}
		return strings.Join(items, ", "), nil
	case map[string]any:
		if v["start"] == "" {
			return "off", nil
		}
		return fmt.Sprintf("%v-%v", v["start"], v["end"]), nil
	case float64:
		return fmt.Sprintf("%d", int(v)), nil
	default:
		return fmt.Sprint(v), nil
	}
}
