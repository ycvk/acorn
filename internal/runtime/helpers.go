package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"github.com/ycvk/acorn/internal/decision"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/stream"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func compactInterruptInfo(value any) any {
	data, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"kind", "message", "question", "action_id", "command", "command_name", "command_args", "cwd", "url", "tool_name", "interrupt_id", "arguments_json", "reason", "rule"} {
		if current, exists := data[key]; exists {
			out[key] = current
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactText(value string, limit int) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	runes := []rune(trimmed)
	if limit <= 0 || len(runes) <= limit {
		return trimmed, false
	}
	return string(runes[:limit]) + "...", true
}

func newRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
}

func newSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UTC().UnixNano())
}

func ExtractString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func InterruptPayloadFromStream(interrupt *stream.StreamInterrupt) map[string]any {
	if interrupt == nil {
		return nil
	}
	payload := map[string]any{"context_count": interrupt.ContextCount}
	contexts := make([]map[string]any, 0, len(interrupt.Contexts))
	for _, item := range interrupt.Contexts {
		contexts = append(contexts, map[string]any{
			"id":            item.ID,
			"address":       item.Address,
			"info":          item.Info,
			"is_root_cause": item.IsRootCause,
		})
	}
	payload["contexts"] = contexts
	return payload
}

func DurableContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func CurrentRunID(ctx context.Context) string {
	return runtimeapi.GetRunID(ctx)
}

func CurrentStreamSink(ctx context.Context) stream.StreamSink {
	return stream.StreamSinkFromContext(ctx)
}

// --- Turn index context plumbing ---

type turnIndexContextKey struct{}

func withTurnIndex(ctx context.Context, turnIndex int) context.Context {
	return context.WithValue(ctx, turnIndexContextKey{}, turnIndex)
}

var defaultEnglishPrinter = message.NewPrinter(language.English)

// ValidationError describes a single schema validation failure.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ToolArgumentValidator compiles and validates tool arguments against JSON Schema.
type ToolArgumentValidator struct {
	toolName string
	schema   *jsonschema.Schema
}

// NewToolArgumentValidator creates a validator from a ToolInfo parameter tree.
// The schema is compiled once at construction time for performance.
func NewToolArgumentValidator(toolName string, params any) (*ToolArgumentValidator, error) {
	schemaBytes, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal tool params for %q: %w", toolName, err)
	}

	var doc any
	if err := json.Unmarshal(schemaBytes, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal tool params for %q: %w", toolName, err)
	}

	compiler := jsonschema.NewCompiler()
	const schemaURL = "internal:///schema"
	if err := compiler.AddResource(schemaURL, doc); err != nil {
		return nil, fmt.Errorf("add schema resource for %q: %w", toolName, err)
	}

	compiled, err := compiler.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile schema for %q: %w", toolName, err)
	}

	return &ToolArgumentValidator{
		toolName: toolName,
		schema:   compiled,
	}, nil
}

// NewToolArgumentValidatorFromToolInfo creates a validator from an Eino ToolInfo.
// It extracts the JSON Schema parameter tree via ToJSONSchema and compiles it.
func NewToolArgumentValidatorFromToolInfo(info *schema.ToolInfo) (*ToolArgumentValidator, error) {
	if info == nil {
		return nil, errors.New("nil ToolInfo")
	}
	js, err := info.ToJSONSchema()
	if err != nil {
		return nil, fmt.Errorf("convert ToolInfo to JSONSchema for %q: %w", info.Name, err)
	}
	if js == nil {
		return &ToolArgumentValidator{toolName: info.Name, schema: nil}, nil
	}
	schemaBytes, err := json.Marshal(js)
	if err != nil {
		return nil, fmt.Errorf("marshal JSONSchema for %q: %w", info.Name, err)
	}
	var params any
	if err := json.Unmarshal(schemaBytes, &params); err != nil {
		return nil, fmt.Errorf("unmarshal JSONSchema for %q: %w", info.Name, err)
	}
	return NewToolArgumentValidator(info.Name, params)
}

// Validate checks raw JSON arguments against the compiled schema.
// Returns structured ValidationErrors (suitable for LLM consumption) or nil if valid.
func (v *ToolArgumentValidator) Validate(argumentsJSON string) ([]ValidationError, error) {
	if strings.TrimSpace(argumentsJSON) == "" {
		argumentsJSON = "{}"
	}

	var args any
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("unmarshal arguments for %q: %w", v.toolName, err)
	}

	if v.schema == nil {
		return nil, nil
	}

	err := v.schema.Validate(args)
	if err == nil {
		return nil, nil
	}

	var verr *jsonschema.ValidationError
	ok := errors.As(err, &verr)
	if !ok {
		return nil, fmt.Errorf("unexpected validation error type for %q: %T", v.toolName, err)
	}

	return flattenValidationErrors(verr), nil
}

// FormatValidationError serializes validation failures into LLM-friendly JSON.
func FormatValidationError(toolName string, errors []ValidationError) string {
	payload := map[string]any{
		"error":   "validation_failed",
		"tool":    toolName,
		"details": errors,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"error":"validation_failed","tool":%q,"marshal_error":%q}`, toolName, err.Error())
	}
	return string(b)
}

// flattenValidationErrors recursively extracts leaf validation errors with
// JSON Pointer field paths and human-readable messages.
func flattenValidationErrors(verr *jsonschema.ValidationError) []ValidationError {
	var out []ValidationError
	collectErrors(verr, &out)
	return out
}

func collectErrors(verr *jsonschema.ValidationError, out *[]ValidationError) {
	if verr == nil {
		return
	}

	// Skip the top-level Schema wrapper; its causes contain the real errors.
	if _, isSchema := verr.ErrorKind.(*kind.Schema); isSchema {
		for _, cause := range verr.Causes {
			collectErrors(cause, out)
		}
		return
	}

	// For Required errors, emit one entry per missing property so the LLM
	// gets a precise field path for each failure.
	if required, ok := verr.ErrorKind.(*kind.Required); ok {
		for _, missing := range required.Missing {
			path := joinJSONPointer(append(append([]string(nil), verr.InstanceLocation...), missing))
			*out = append(*out, ValidationError{
				Field:   path,
				Message: defaultEnglishPrinter.Sprintf("missing required field %s", quoteJSONSchema(missing)),
			})
		}
		return
	}

	// Emit a leaf error for everything else.
	*out = append(*out, ValidationError{
		Field:   joinJSONPointer(verr.InstanceLocation),
		Message: verr.ErrorKind.LocalizedString(defaultEnglishPrinter),
	})

	// Some error kinds (e.g. AllOf, AnyOf) have nested causes that also
	// contain actionable detail; recurse into them.
	for _, cause := range verr.Causes {
		collectErrors(cause, out)
	}
}

// joinJSONPointer turns a slice of JSON value path segments into a JSON Pointer
// string (e.g. []string{"path"} -> "/path", []string{} -> "").
func joinJSONPointer(segments []string) string {
	if len(segments) == 0 {
		return ""
	}
	return "/" + strings.Join(segments, "/")
}

// quoteJSONSchema returns a quoted string using the same quoting rules that
// jsonschema/v6 uses in its error messages.
func quoteJSONSchema(s string) string {
	return fmt.Sprintf("%q", s)
}

func buildDecisionInput(
	req RunnerBuildRequest,
	matches []SkillMatch,
	hasWorkingContext bool,
) decision.DecideInput {
	return decision.DecideInput{
		RunID:             req.RunID,
		SessionID:         req.SessionID,
		Input:             req.Input,
		ExplicitSkillID:   req.SkillID,
		HasWorkingContext: hasWorkingContext,
		AvailableSkills:   recommendedSkillsFromMatches(matches),
	}
}

func buildDecisionEngine(profile *decision.ProfileService) (*decision.Engine, *decision.ParsedProfile, error) {
	if profile == nil {
		return nil, nil, fmt.Errorf("decision profile service is nil")
	}
	parsed, err := profile.Load()
	if err != nil {
		return nil, nil, err
	}
	engine := decision.NewEngine(parsed.Profile)
	return engine, parsed, nil
}

func fillRecordMetadata(record *decision.Record, profileHash string) {
	if record == nil {
		return
	}
	record.DecisionProfileHash = profileHash
	record.CreatedAt = time.Now().UTC()
}

func selectedSkillFromDecisionRecord(record *decision.Record, matches []SkillMatch, stableSkills []skills.Spec) (*SelectedSkill, error) {
	if record == nil {
		return nil, nil
	}
	if record.Action != decision.ActionExecuteWithSkill {
		return nil, nil
	}
	skillID := strings.TrimSpace(record.SelectedSkillID)
	if skillID == "" {
		return nil, fmt.Errorf("decision action execute_with_skill requires selected skill id")
	}
	score, matchedTerms := selectedSkillMatchMetadata(skillID, matches)
	for _, item := range stableSkills {
		if item.ID != skillID {
			continue
		}
		return &SelectedSkill{
			Skill:        skills.CopySpec(item),
			Score:        score,
			MatchedTerms: matchedTerms,
			Explicit:     record.DecisionReason == "explicit_skill",
		}, nil
	}
	return nil, fmt.Errorf("decision selected skill %q not found", skillID)
}

func selectedSkillMatchMetadata(skillID string, matches []SkillMatch) (int, []string) {
	for _, match := range matches {
		if match.Skill.ID == skillID {
			return match.Score, append([]string(nil), match.MatchedTerms...)
		}
	}
	return 0, nil
}

type JSONSerializer struct{}

func (j *JSONSerializer) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (j *JSONSerializer) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

var _ compose.Serializer = (*JSONSerializer)(nil)
