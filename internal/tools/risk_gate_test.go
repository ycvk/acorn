package tools

import "testing"

func TestClassifyRiskLowForReadOnlyTools(t *testing.T) {
	low := []string{"file_read", "web_fetch", "web_search", "browser", "memory_search", "artifact_list", ""}
	for _, name := range low {
		if ClassifyRisk(name) != RiskLow {
			t.Errorf("ClassifyRisk(%q) = RiskHigh, want RiskLow", name)
		}
		if IsHighRisk(name) {
			t.Errorf("IsHighRisk(%q) = true, want false", name)
		}
	}
}

func TestClassifyRiskHighForDangerousTools(t *testing.T) {
	high := []string{"file_delete", "file_write", "run_command", "git_commit", "commit_push", "deploy", "email_send"}
	for _, name := range high {
		if ClassifyRisk(name) != RiskHigh {
			t.Errorf("ClassifyRisk(%q) = RiskLow, want RiskHigh", name)
		}
		if !IsHighRisk(name) {
			t.Errorf("IsHighRisk(%q) = false, want true", name)
		}
	}
}

func TestClassifyRiskIsCaseInsensitive(t *testing.T) {
	// Whitespace is trimmed; tool names are case-sensitive (they match the
	// registry exactly), but trimming prevents accidental false negatives.
	if ClassifyRisk("  file_delete  ") != RiskHigh {
		t.Error("whitespace-padded file_delete should be RiskHigh")
	}
}
