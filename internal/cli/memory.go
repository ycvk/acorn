package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/memorymodule"
)

func runMemory(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("memory requires a subcommand: procedure or semantic")
	}
	switch args[0] {
	case "procedure":
		return runMemoryProcedure(ctx, args[1:])
	case "semantic":
		return runMemorySemantic(ctx, args[1:])
	default:
		return fmt.Errorf("unknown memory subcommand %q", args[0])
	}
}

func runMemorySemantic(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("memory semantic requires a subcommand: rebuild")
	}
	switch args[0] {
	case "rebuild":
		return runMemorySemanticRebuild(ctx, args[1:])
	default:
		return fmt.Errorf("unknown memory semantic subcommand %q", args[0])
	}
}

func runMemorySemanticRebuild(ctx context.Context, args []string) error {
	fs := newFlagSet("memory semantic rebuild")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print semantic rebuild receipt as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("memory semantic rebuild does not accept positional arguments")
	}
	return withContainer(ctx, *configPath, func(container *app.Container) error {
		result, err := container.Memory().RebuildSemanticIndex(ctx)
		if err != nil {
			return err
		}
		if *jsonMode {
			return printJSON(result)
		}
		fmt.Println(renderSemanticRebuildResult(result))
		return nil
	})
}

func runMemoryProcedure(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("memory procedure requires a subcommand: create")
	}
	switch args[0] {
	case "create":
		return runMemoryProcedureCreate(ctx, args[1:])
	default:
		return fmt.Errorf("unknown memory procedure subcommand %q", args[0])
	}
}

func runMemoryProcedureCreate(ctx context.Context, args []string) error {
	fs := newFlagSet("memory procedure create")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print created procedure as JSON")
	title := fs.String("title", "", "procedure title")
	taskPattern := fs.String("task-pattern", "", "comma-separated task pattern")
	sourceRun := fs.String("source-run", "", "source run id")
	sourceRefs := fs.String("source-refs", "", "comma-separated source refs")
	evidenceRefs := fs.String("evidence-refs", "", "comma-separated evidence refs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("memory procedure create requires procedure body text")
	}
	body := strings.Join(fs.Args(), " ")
	return withContainer(ctx, *configPath, func(container *app.Container) error {
		procedure, err := container.Memory().CreateProcedure(ctx, memorymodule.CreateProcedureRequest{
			Title:        *title,
			TaskPattern:  *taskPattern,
			Body:         body,
			SourceRun:    *sourceRun,
			SourceRefs:   splitRefList(*sourceRefs),
			EvidenceRefs: splitRefList(*evidenceRefs),
		})
		if err != nil {
			return err
		}
		if *jsonMode {
			return printJSON(procedure)
		}
		fmt.Println(renderProcedureCreated(procedure))
		return nil
	})
}

func splitRefList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func renderProcedureCreated(item *memorymodule.ProcedureRecord) string {
	if item == nil {
		return "Procedure created."
	}
	lines := []string{
		"Procedure created",
		"  Ref: " + item.Ref,
		"  Status: " + string(item.Status),
		"  Origin: " + string(item.Origin),
		"  Source run: " + item.SourceRun,
	}
	if item.MutationPlan != nil {
		lines = append(lines, "  Mutation plan: "+string(item.MutationPlan.Action))
	}
	if len(item.EvidenceRefs) > 0 {
		lines = append(lines, "  Evidence refs: "+strings.Join(item.EvidenceRefs, ", "))
	}
	return strings.Join(lines, "\n")
}

func renderSemanticRebuildResult(result *memorymodule.SemanticRebuildResult) string {
	if result == nil {
		return "Semantic index rebuild complete."
	}
	lines := []string{
		"Semantic index rebuild complete",
		"  Index: " + result.IndexName,
		"  Schema: " + result.Schema,
		"  Model: " + result.Model,
		fmt.Sprintf("  Dimensions: %d", result.Dimensions),
		fmt.Sprintf("  Indexed: %d", result.IndexedCount),
		fmt.Sprintf("  Deleted: %d", result.DeletedCount),
		fmt.Sprintf("  Skipped: %d", result.SkippedCount),
	}
	return strings.Join(lines, "\n")
}
