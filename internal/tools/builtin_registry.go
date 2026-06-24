package tools

import (
	"github.com/ycvk/acorn/internal/port"
)

// builtinToolOrder is the canonical list of dynamically-registered built-in
// tools (delegate_task, load_tools, working-state, memory, skill). It is the
// single source of truth for built-in tool identity: BuiltinToolNames and the
// runtime spec resolver (tool.RuntimeToolSpec via BuiltinToolSpec) both derive
// from it, so adding a built-in tool means editing this one place.
//
// Static local tools (read_file, create_file, run_command, ...) are declared
// separately in localToolDefs/configuredLocalSpec.
var builtinToolOrder = []string{
	"memory_search",
	"memory_read_file",
	"memory_list_files",
	"memory_create_file",
	"memory_replace_span",
	"remember",
	"skill_list",
	"skill_view",
	"load_tools",
	"ask_operator",
	"update_working_checkpoint",
	"clear_working_checkpoint",
}

// builtinToolContract returns the contract template (without Source/Profiles,
// which are caller-supplied) for a built-in tool. ok is false for any name that
// is not a built-in tool (e.g. MCP tools), which callers resolve elsewhere.
func builtinToolContract(name string) (port.ToolContract, bool) {
	c := port.ToolContract{
		Name:      name,
		Loading:   port.EagerLoadingPolicy(),
		Execution: port.ToolExecutionPolicy{ParallelPolicy: port.ParallelPolicyReadOnly},
	}
	switch name {
	case "load_tools":
		c.Kind = port.ToolKindNative
		c.Category = port.ToolCategoryInspect
		c.Execution.ParallelPolicy = port.ParallelPolicySerial
	case "ask_operator":
		c.Kind = port.ToolKindNative
		c.Category = port.ToolCategoryIntegration
		c.Execution.ParallelPolicy = port.ParallelPolicySerial
	case "update_working_checkpoint", "clear_working_checkpoint":
		c.Kind = port.ToolKindMemory
		c.Category = port.ToolCategoryMemory
		c.Loading = port.DeferredLoadingPolicy("working_state_tool")
		c.Execution.ParallelPolicy = port.ParallelPolicySerial
	case "memory_search", "memory_read_file", "memory_list_files":
		c.Kind = port.ToolKindMemory
		c.Category = port.ToolCategoryMemory
		c.Execution.ParallelPolicy = port.ParallelPolicyReadOnly
	case "memory_create_file", "memory_replace_span":
		c.Kind = port.ToolKindMemory
		c.Category = port.ToolCategoryMemory
		c.Execution.ParallelPolicy = port.ParallelPolicySerial
		c.Execution.PathArg = "path"
	case "remember":
		c.Kind = port.ToolKindMemory
		c.Category = port.ToolCategoryMemory
		c.Execution.ParallelPolicy = port.ParallelPolicySerial
	case "skill_list", "skill_view":
		c.Kind = port.ToolKindSkill
		c.Category = port.ToolCategorySkill
		c.Execution.ParallelPolicy = port.ParallelPolicyReadOnly
	default:
		return port.ToolContract{}, false
	}
	return c, true
}

// BuiltinToolSpec resolves the full contract for a built-in tool, applying the
// caller-supplied source to the canonical contract template. It returns ok=false
// for names that are not built-in toolset.
func BuiltinToolSpec(name, source string) (port.ToolContract, bool) {
	c, ok := builtinToolContract(name)
	if !ok {
		return port.ToolContract{}, false
	}
	c.Source = source
	return c, true
}

// BuiltinToolNames returns the built-in tools that are always eligible for skill
// matching, i.e. the eager-loaded built-ins. Deferred built-ins (working-state
// tools) are loaded on demand and are intentionally excluded. The list derives
// from builtinToolOrder, so it never drifts from the contract registry.
func BuiltinToolNames() []string {
	names := make([]string, 0, len(builtinToolOrder))
	for _, name := range builtinToolOrder {
		contract, ok := builtinToolContract(name)
		if ok && contract.Loading.Mode == port.ToolLoadingModeEager {
			names = append(names, name)
		}
	}
	return names
}
