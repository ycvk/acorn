package skills

import (
	"slices"
	"strings"
)

// EligibilityContext is what a run offers a skill.
type EligibilityContext struct {
	AvailableTools []string
}

// filesTool reads a skill's files on the workstation.
const filesTool = "files"

// MCPToolPrefix starts the names of the tools a skill's MCP server provides.
func MCPToolPrefix(skill string) string {
	return "mcp__" + skill + "__"
}

func BuildSnapshot(scan ScanResult, ctx EligibilityContext) Snapshot {
	views := make([]View, 0, len(scan.Skills))
	for _, item := range scan.Skills {
		views = append(views, Evaluate(item, ctx))
	}
	return Snapshot{Skills: views, Problems: slices.Clone(scan.Problems)}
}

// Evaluate says whether a run can use the skill: it has every required tool,
// and the workstation's files tool when the skill carries files. The tools of
// the skill's own MCP server are checked once the server is connected.
func Evaluate(item Spec, ctx EligibilityContext) View {
	var reasons []string
	var missing []string
	for _, tool := range item.RequiresTools {
		if item.MCP != nil && strings.HasPrefix(tool, MCPToolPrefix(item.Name)) {
			continue
		}
		if !slices.Contains(ctx.AvailableTools, tool) {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		reasons = append(reasons, "missing_required_tools:"+strings.Join(missing, ","))
	}
	if len(item.Files) > 0 && !slices.Contains(ctx.AvailableTools, filesTool) {
		reasons = append(reasons, "files_need_workstation")
	}
	return View{Spec: CopySpec(item), Eligible: len(reasons) == 0, DisabledReasons: reasons}
}
