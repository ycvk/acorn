package skills

func testEligibleCtx() EligibilityContext {
	return EligibilityContext{
		AvailableTools:    []string{"read_file", "search_text", "create_file", "replace_span", "apply_unified_patch", "run_command"},
		AvailableToolsets: []string{"filesystem", "shell"},
		GOOS:              "darwin",
		Env:               map[string]string{"HOME": "/tmp", "PATH": "/usr/bin"},
		LookPath:          func(string) (string, error) { return "/usr/bin/tool", nil },
	}
}
