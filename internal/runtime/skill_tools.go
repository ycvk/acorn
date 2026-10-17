package runtime

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/ycvk/acorn/internal/core"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	"github.com/ycvk/acorn/internal/skills"
)

// serveSkillTools has the workstation run the MCP servers of the run's
// skills and connects to each one for this run; the connections close when
// ctx ends. A skill whose server does not serve leaves the run with the
// reason, and its tools with it. Without a workstation, skills that declare a
// server are already out of the run, as they carry files.
func serveSkillTools(ctx context.Context, deps RuntimeDeps, snapshot skills.Snapshot) (skills.Snapshot, []core.ToolSpec) {
	if deps.SkillServers == nil {
		return snapshot, nil
	}
	served, err := deps.Skills.Serve(ctx, deps.SkillServers, snapshot)
	if err != nil {
		slog.Warn("skill MCP servers unavailable", "error", err)
		out := skills.CopySnapshot(snapshot)
		for i, view := range out.Skills {
			if view.Eligible && view.MCP != nil {
				out.Skills[i] = skills.Disable(view, "mcp_unavailable")
			}
		}
		return out, nil
	}
	var specs []core.ToolSpec
	for i, view := range served.Skills {
		if !view.Eligible || view.MCP == nil {
			continue
		}
		toolSpecs, reason := connectSkillTools(ctx, deps, view)
		if reason != "" {
			slog.Warn("skill MCP server left out of the run", "skill", view.Name, "reason", reason)
			served.Skills[i] = skills.Disable(view, reason)
			continue
		}
		specs = append(specs, toolSpecs...)
	}
	return served, specs
}

// connectSkillTools connects to a skill's MCP server and returns its tools
// as mcp__<skill>__<tool>, or why the skill cannot use them.
func connectSkillTools(ctx context.Context, deps RuntimeDeps, view skills.View) ([]core.ToolSpec, string) {
	conn, err := mcpprovider.Connect(ctx, deps.SkillServers.MCPProvider(view.Name))
	if err != nil {
		return nil, "mcp_connect_failed:" + err.Error()
	}
	specs := make([]core.ToolSpec, 0, len(conn.Tools()))
	names := make([]string, 0, len(conn.Tools()))
	for _, tool := range conn.Tools() {
		spec, err := buildMCPToolSpec(ctx, deps.Config, view.Name, tool)
		if err != nil {
			return nil, closeFor(conn, view.Name, "mcp_tool_invalid:"+err.Error())
		}
		specs = append(specs, spec)
		names = append(names, spec.Name)
	}
	var missing []string
	for _, tool := range view.RequiresTools {
		if strings.HasPrefix(tool, skills.MCPToolPrefix(view.Name)) && !slices.Contains(names, tool) {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return nil, closeFor(conn, view.Name, "missing_required_tools:"+strings.Join(missing, ","))
	}
	context.AfterFunc(ctx, func() {
		if err := conn.Close(); err != nil {
			slog.Warn("close skill MCP connection", "skill", view.Name, "error", err)
		}
	})
	return specs, ""
}

// closeFor closes a connection the run will not use and returns reason.
func closeFor(conn *mcpprovider.Connection, skill, reason string) string {
	if err := conn.Close(); err != nil {
		slog.Warn("close skill MCP connection", "skill", skill, "error", err)
	}
	return reason
}

// ProviderNamespaces are the names the configured MCP providers' tools are
// namespaced under, which skill packages' MCP servers cannot take.
func ProviderNamespaces(providers []core.ProviderConfig) []string {
	out := make([]string, 0, len(providers))
	for _, p := range providers {
		if strings.TrimSpace(p.Name) == "" {
			continue
		}
		out = append(out, sanitizeMCPProviderName(p.Name))
	}
	return out
}
