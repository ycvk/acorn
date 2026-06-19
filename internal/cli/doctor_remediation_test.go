package cli

import (
	"strings"
	"testing"
)

func TestDoctorRemediationLinesAlwaysGuideToConfigAndInit(t *testing.T) {
	lines := strings.Join(doctorRemediationLines("agent.max_iterations must be > 0", "/etc/acorn/acorn.yaml"), "\n")
	for _, want := range []string{"Fix:", "/etc/acorn/acorn.yaml", "acorn doctor", "acorn init"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("remediation missing %q:\n%s", want, lines)
		}
	}
	// A non-api_key reason must NOT emit the misleading env-var/api_key hint.
	if strings.Contains(lines, "OPENAI_API_KEY") {
		t.Fatalf("max_iterations remediation should not mention OPENAI_API_KEY:\n%s", lines)
	}
}

func TestDoctorRemediationLinesTailorsApiKeyHints(t *testing.T) {
	providerKey := strings.Join(doctorRemediationLines("provider primary: api_key is required", ""), "\n")
	if !strings.Contains(providerKey, "OPENAI_API_KEY") {
		t.Fatalf("provider api_key remediation should mention OPENAI_API_KEY:\n%s", providerKey)
	}

	embeddingKey := strings.Join(doctorRemediationLines("memory.semantic.embedding.api_key is required", ""), "\n")
	if !strings.Contains(embeddingKey, "model+base_url") {
		t.Fatalf("embedding api_key remediation should offer disabling semantic:\n%s", embeddingKey)
	}
}
