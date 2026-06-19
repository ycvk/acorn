package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestExecutionReadinessBanner(t *testing.T) {
	if got := executionReadinessBanner(nil); got != "Execution: ready" {
		t.Fatalf("ready banner = %q, want %q", got, "Execution: ready")
	}

	got := executionReadinessBanner(errors.New("provider primary: api_key is required"))
	for _, want := range []string{"NOT READY", "execution_not_ready", "api_key is required", "acorn doctor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("not-ready banner missing %q:\n%s", want, got)
		}
	}
}
