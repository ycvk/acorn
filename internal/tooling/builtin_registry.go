package tooling

// BuiltinToolNames returns the list of built-in tool names that are always
// eligible for skill matching. This is the canonical registry for built-in
// tools and should be kept in sync with the actual tool implementations.
func BuiltinToolNames() []string {
	return []string{
		"delegate_task",
		"memory_search",
		"memory_read_file",
		"memory_list_files",
		"memory_create_file",
		"memory_replace_span",
		"skill_list",
		"skill_view",
		"load_tools",
	}
}
