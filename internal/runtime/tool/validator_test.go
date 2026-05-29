package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/runtime/tooltest"
)

func TestValidatorValidArgumentsPass(t *testing.T) {
	tool := tooltest.MustInferTool(t, "write_file", func(ctx context.Context, input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}) (string, error) {
		return "ok", nil
	})
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}

	v, err := newToolArgumentValidatorFromToolInfo(info)
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	errors, err := v.validate(`{"path":"/tmp/test","content":"hello"}`)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errors) != 0 {
		t.Fatalf("expected no errors, got %v", errors)
	}
}

func TestValidatorMissingRequiredField(t *testing.T) {
	tool := tooltest.MustInferTool(t, "write_file", func(ctx context.Context, input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}) (string, error) {
		return "ok", nil
	})
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}

	v, err := newToolArgumentValidatorFromToolInfo(info)
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	errors, err := v.validate(`{}`)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errors) == 0 {
		t.Fatal("expected errors for missing required fields, got none")
	}

	fields := make(map[string]string, len(errors))
	for _, e := range errors {
		fields[e.Field] = e.Message
	}

	if _, ok := fields["/path"]; !ok {
		t.Fatalf("expected error for field /path, got fields %v", fields)
	}
	if _, ok := fields["/content"]; !ok {
		t.Fatalf("expected error for field /content, got fields %v", fields)
	}

	for field, msg := range fields {
		if !strings.Contains(strings.ToLower(msg), "missing") && !strings.Contains(strings.ToLower(msg), "required") {
			t.Fatalf("field %q message %q should mention missing or required", field, msg)
		}
	}
}

func TestValidatorWrongType(t *testing.T) {
	tool := tooltest.MustInferTool(t, "write_file", func(ctx context.Context, input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}) (string, error) {
		return "ok", nil
	})
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}

	v, err := newToolArgumentValidatorFromToolInfo(info)
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	errors, err := v.validate(`{"path":123,"content":"hello"}`)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errors) == 0 {
		t.Fatal("expected type error, got none")
	}

	foundPath := false
	for _, e := range errors {
		if e.Field == "/path" && strings.Contains(strings.ToLower(e.Message), "string") {
			foundPath = true
		}
	}
	if !foundPath {
		t.Fatalf("expected type error for /path mentioning string, got %v", errors)
	}
}

func TestValidatorEnumConstraint(t *testing.T) {
	tool := tooltest.MustInferTool(t, "set_mode", func(ctx context.Context, input struct {
		Mode string `json:"mode" jsonschema:"enum=read,enum=write,enum=admin"`
	}) (string, error) {
		return "ok", nil
	})
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}

	v, err := newToolArgumentValidatorFromToolInfo(info)
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	errors, err := v.validate(`{"mode":"read"}`)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errors) != 0 {
		t.Fatalf("expected no errors for valid enum, got %v", errors)
	}

	errors, err = v.validate(`{"mode":"invalid"}`)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errors) == 0 {
		t.Fatal("expected enum error, got none")
	}

	found := false
	for _, e := range errors {
		if e.Field == "/mode" && (strings.Contains(strings.ToLower(e.Message), "enum") || strings.Contains(strings.ToLower(e.Message), "one of")) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected enum error for /mode, got %v", errors)
	}
}

func TestValidatorNestedObject(t *testing.T) {
	type Inner struct {
		Field string `json:"field"`
	}
	tool := tooltest.MustInferTool(t, "nested_tool", func(ctx context.Context, input struct {
		Inner Inner `json:"inner"`
	}) (string, error) {
		return "ok", nil
	})
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}

	v, err := newToolArgumentValidatorFromToolInfo(info)
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	errors, err := v.validate(`{"inner":{}}`)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errors) == 0 {
		t.Fatal("expected nested error, got none")
	}

	found := false
	for _, e := range errors {
		if e.Field == "/inner/field" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error at /inner/field, got %v", errors)
	}
}

func TestValidatorEmptyArgumentsWithNoRequiredFields(t *testing.T) {
	tool := tooltest.MustInferTool(t, "optional_tool", func(ctx context.Context, input struct {
		Path    string `json:"path,omitempty"`
		Content string `json:"content,omitempty"`
	}) (string, error) {
		return "ok", nil
	})
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}

	v, err := newToolArgumentValidatorFromToolInfo(info)
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	errors, err := v.validate(`{}`)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(errors) != 0 {
		t.Fatalf("expected no errors for empty optional args, got %v", errors)
	}

	errors, err = v.validate("")
	if err != nil {
		t.Fatalf("validate empty string: %v", err)
	}
	if len(errors) != 0 {
		t.Fatalf("expected no errors for empty-string args, got %v", errors)
	}
}

func TestValidatorComplexRealToolSchema(t *testing.T) {
	type FileWriteInput struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Mode    string `json:"mode"`
	}
	tool := tooltest.MustInferTool(t, "create_file", func(ctx context.Context, input FileWriteInput) (string, error) {
		return "ok", nil
	})
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("tool info: %v", err)
	}

	v, err := newToolArgumentValidatorFromToolInfo(info)
	if err != nil {
		t.Fatalf("new validator: %v", err)
	}

	errors, err := v.validate(`{"path":"/tmp/foo","content":"hello","mode":"overwrite"}`)
	if err != nil {
		t.Fatalf("validate valid: %v", err)
	}
	if len(errors) != 0 {
		t.Fatalf("expected no errors for valid input, got %v", errors)
	}

	errors, err = v.validate(`{"mode":"overwrite"}`)
	if err != nil {
		t.Fatalf("validate missing: %v", err)
	}
	if len(errors) == 0 {
		t.Fatal("expected errors for missing fields, got none")
	}

	fieldSet := make(map[string]struct{}, len(errors))
	for _, e := range errors {
		fieldSet[e.Field] = struct{}{}
	}
	if _, ok := fieldSet["/path"]; !ok {
		t.Fatalf("expected error for /path, got fields %v", fieldSet)
	}
	if _, ok := fieldSet["/content"]; !ok {
		t.Fatalf("expected error for /content, got fields %v", fieldSet)
	}
}

func TestValidatorFormatValidationError(t *testing.T) {
	errors := []validationError{
		{Field: "/path", Message: "missing required field \"path\""},
		{Field: "/content", Message: "expected string, got number"},
	}
	jsonStr := formatValidationError("create_file", errors)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("parse validation error JSON: %v", err)
	}
	if parsed["error"] != "validation_failed" {
		t.Fatalf("expected error=validation_failed, got %v", parsed["error"])
	}
	if parsed["tool"] != "create_file" {
		t.Fatalf("expected tool=create_file, got %v", parsed["tool"])
	}

	details, ok := parsed["details"].([]any)
	if !ok || len(details) != 2 {
		t.Fatalf("expected 2 details, got %v", parsed["details"])
	}
}
