package tooling

// builtinToolOrder is the canonical list of dynamically-registered built-in
// tools (delegate_task, load_tools, working-state, memory, skill). It is the
// single source of truth for built-in tool identity: BuiltinToolNames and the
// runtime spec resolver (tool.RuntimeToolSpec via BuiltinToolSpec) both derive
// from it, so adding a built-in tool means editing this one place.
//
// Static local tools (read_file, create_file, run_command, ...) are declared
// separately in localToolDefs/configuredLocalSpec.
var builtinToolOrder = []string{
	"delegate_task",
	"memory_search",
	"memory_read_file",
	"memory_list_files",
	"memory_create_file",
	"memory_replace_span",
	"remember",
	"skill_list",
	"skill_view",
	"load_tools",
	"update_working_checkpoint",
	"clear_working_checkpoint",
}

// builtinToolContract returns the contract template (without Source/Profiles,
// which are caller-supplied) for a built-in tool. ok is false for any name that
// is not a built-in tool (e.g. MCP tools), which callers resolve elsewhere.
func builtinToolContract(name string) (ToolContract, bool) {
	c := ToolContract{
		Name:       name,
		PlanPolicy: PlanPolicyNone,
		Loading:    EagerLoadingPolicy(),
		Execution:  ToolExecutionPolicy{ParallelPolicy: ParallelPolicyReadOnly},
	}
	switch name {
	case "delegate_task":
		c.Kind = ToolKindSkill
		c.Category = ToolCategorySkill
		c.ResourceScope = ResourceScopeSkill
		c.Execution.ParallelPolicy = ParallelPolicyNeverParallel
		c.PlanPolicy = PlanPolicyRequireActivePlan
	case "load_tools":
		c.Kind = ToolKindNative
		c.Category = ToolCategoryInspect
		c.ResourceScope = ResourceScopeWorkspaceFile
		c.Execution.ParallelPolicy = ParallelPolicyNeverParallel
	case "update_working_checkpoint", "clear_working_checkpoint":
		c.Kind = ToolKindMemory
		c.Category = ToolCategoryMemory
		c.ResourceScope = ResourceScopeMemory
		c.Loading = DeferredLoadingPolicy("working_state_tool")
		c.Execution.ParallelPolicy = ParallelPolicyNeverParallel
	case "memory_search", "memory_read_file", "memory_list_files":
		c.Kind = ToolKindMemory
		c.Category = ToolCategoryMemory
		c.ResourceScope = ResourceScopeMemory
		c.Execution.ParallelPolicy = ParallelPolicyReadOnly
	case "memory_create_file", "memory_replace_span":
		c.Kind = ToolKindMemory
		c.Category = ToolCategoryMemory
		c.ResourceScope = ResourceScopeMemory
		c.Execution.ParallelPolicy = ParallelPolicyWriteScoped
		c.Execution.PathArg = "path"
	case "remember":
		// Structured fact writer: the file path is backend-derived (no path arg),
		// so it cannot be path-scoped; serialize memory writes instead.
		c.Kind = ToolKindMemory
		c.Category = ToolCategoryMemory
		c.ResourceScope = ResourceScopeMemory
		c.Execution.ParallelPolicy = ParallelPolicyNeverParallel
	case "skill_list", "skill_view":
		c.Kind = ToolKindSkill
		c.Category = ToolCategorySkill
		c.ResourceScope = ResourceScopeSkill
		c.Execution.ParallelPolicy = ParallelPolicyReadOnly
	default:
		return ToolContract{}, false
	}
	return c, true
}

// BuiltinToolSpec resolves the full contract for a built-in tool, applying the
// caller-supplied source and profiles to the canonical contract template. It
// returns ok=false for names that are not built-in tools.
func BuiltinToolSpec(name, source string, profiles []ToolProfile) (ToolContract, bool) {
	c, ok := builtinToolContract(name)
	if !ok {
		return ToolContract{}, false
	}
	c.Source = source
	c.Profiles = append([]ToolProfile(nil), profiles...)
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
		if ok && contract.Loading.Mode == ToolLoadingModeEager {
			names = append(names, name)
		}
	}
	return names
}
