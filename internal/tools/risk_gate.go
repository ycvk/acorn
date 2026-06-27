package tools

import (
	"strings"
)

// RiskLevel classifies a tool call's potential for irreversible or costly
// side effects. The risk gate uses this to decide auto-approve vs escalate
// to operator approval.
type RiskLevel int

const (
	// RiskLow: read-only or harmless operations. Auto-approved.
	RiskLow RiskLevel = iota
	// RiskHigh: costly, external, or irreversible operations. Must escalate
	// to operator approval via ask_operator.
	RiskHigh
)

// highRiskTools is the hardcoded set of tools whose invocation always
// requires operator approval. Per ADR-0001 §已锁定的实施决定 #6:
// "任何花钱、任何对外发送、任何删除、任何部署变更、任何 SaaS 账户操作"
// This list is intentionally a rule, not an LLM judgment, and cannot be
// bypassed by the agent.
var highRiskTools = map[string]bool{
	// deletion
	"file_delete": true,
	"git_delete":  true,
	// external sending
	"email_send":   true,
	"message_send": true,
	"commit_push":  true,
	// deployment / SaaS mutations
	"deploy":       true,
	"saas_account": true,
	"run_command":  true, // shell commands can do anything
	// workspace mutations
	"file_write": true,
	"file_move":  true,
	"git_commit": true,
}

// ClassifyRisk returns the risk level for a tool call. It is a pure rule
// lookup — no LLM involvement — and is the single gate that decides whether
// a tool call auto-runs or escalates to operator approval.
func ClassifyRisk(toolName string) RiskLevel {
	if highRiskTools[strings.TrimSpace(toolName)] {
		return RiskHigh
	}
	return RiskLow
}

// IsHighRisk is a convenience predicate for callers that only need the
// binary decision.
func IsHighRisk(toolName string) bool {
	return ClassifyRisk(toolName) == RiskHigh
}
