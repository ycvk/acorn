package skills

import (
	"os"
	"os/exec"
	goRuntime "runtime"
	"strings"
)

type EligibilityContext struct {
	AvailableTools    []string
	AvailableToolsets []string
	GOOS              string
	Env               map[string]string
	LookPath          func(string) (string, error)
}

func BuildSnapshot(scan ScanResult, ctx EligibilityContext) (Snapshot, error) {
	views := make([]View, 0, len(scan.Skills))
	for _, item := range scan.Skills {
		view, err := Evaluate(item, ctx)
		if err != nil {
			return Snapshot{}, err
		}
		views = append(views, view)
	}
	return Snapshot{
		Skills:   views,
		Problems: append([]Problem(nil), scan.Problems...),
	}, nil
}

func Evaluate(item Spec, ctx EligibilityContext) (View, error) {
	current, err := NormalizeSpec(item)
	if err != nil {
		return View{}, err
	}
	ctx = normalizeEligibilityContext(ctx)
	reasons := make([]string, 0, 4)
	available := make(map[string]struct{}, len(ctx.AvailableTools))
	for _, toolName := range uniqueNonEmpty(ctx.AvailableTools) {
		available[toolName] = struct{}{}
	}
	availableToolsets := make(map[string]struct{}, len(ctx.AvailableToolsets))
	for _, toolset := range uniqueNonEmpty(ctx.AvailableToolsets) {
		availableToolsets[toolset] = struct{}{}
	}
	if missing := missingRequiredTools(current.Requires.Tools, available); len(missing) > 0 {
		reasons = append(reasons, "missing_required_tools:"+strings.Join(missing, ","))
	}
	if missing := missingRequiredTools(current.Requires.Toolsets, availableToolsets); len(missing) > 0 {
		reasons = append(reasons, "missing_required_toolsets:"+strings.Join(missing, ","))
	}
	if len(current.Platforms) > 0 && ctx.GOOS != "" && !containsString(current.Platforms, ctx.GOOS) {
		reasons = append(reasons, "unsupported_platform:"+strings.Join(current.Platforms, ","))
	}
	if missing := missingRequiredBins(current.Requires.Bins, ctx.LookPath); len(missing) > 0 {
		reasons = append(reasons, "missing_required_bins:"+strings.Join(missing, ","))
	}
	if missing := missingRequiredEnv(current.Requires.Env, ctx.Env); len(missing) > 0 {
		reasons = append(reasons, "missing_required_env:"+strings.Join(missing, ","))
	}
	return View{
		Spec:            current,
		Eligible:        len(reasons) == 0,
		DisabledReasons: reasons,
	}, nil
}

func normalizeEligibilityContext(ctx EligibilityContext) EligibilityContext {
	if strings.TrimSpace(ctx.GOOS) == "" {
		ctx.GOOS = goRuntime.GOOS
	}
	if ctx.Env == nil {
		ctx.Env = currentEnvMap()
	}
	if ctx.LookPath == nil {
		ctx.LookPath = exec.LookPath
	}
	ctx.AvailableTools = uniqueNonEmpty(ctx.AvailableTools)
	ctx.AvailableToolsets = uniqueNonEmpty(ctx.AvailableToolsets)
	return ctx
}

func missingRequiredTools(required []string, available map[string]struct{}) []string {
	missing := make([]string, 0)
	for _, item := range uniqueNonEmpty(required) {
		if _, ok := available[item]; ok {
			continue
		}
		missing = append(missing, item)
	}
	return missing
}

func missingRequiredBins(required []string, lookPath func(string) (string, error)) []string {
	if lookPath == nil {
		return uniqueNonEmpty(required)
	}
	missing := make([]string, 0)
	for _, item := range uniqueNonEmpty(required) {
		if _, err := lookPath(item); err == nil {
			continue
		}
		missing = append(missing, item)
	}
	return missing
}

func missingRequiredEnv(required []string, env map[string]string) []string {
	missing := make([]string, 0)
	for _, item := range uniqueNonEmpty(required) {
		if strings.TrimSpace(env[item]) != "" {
			continue
		}
		missing = append(missing, item)
	}
	return missing
}

func currentEnvMap() map[string]string {
	items := os.Environ()
	out := make(map[string]string, len(items))
	for _, item := range items {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}
		out[parts[0]] = parts[1]
	}
	return out
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}
