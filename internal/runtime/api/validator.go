package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

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

func flattenValidationErrors(verr *jsonschema.ValidationError) []ValidationError {
	var out []ValidationError
	collectErrors(verr, &out)
	return out
}

func collectErrors(verr *jsonschema.ValidationError, out *[]ValidationError) {
	if verr == nil {
		return
	}

	if _, isSchema := verr.ErrorKind.(*kind.Schema); isSchema {
		for _, cause := range verr.Causes {
			collectErrors(cause, out)
		}
		return
	}

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

	*out = append(*out, ValidationError{
		Field:   joinJSONPointer(verr.InstanceLocation),
		Message: verr.ErrorKind.LocalizedString(defaultEnglishPrinter),
	})

	for _, cause := range verr.Causes {
		collectErrors(cause, out)
	}
}

func joinJSONPointer(segments []string) string {
	if len(segments) == 0 {
		return ""
	}
	return "/" + strings.Join(segments, "/")
}

func quoteJSONSchema(s string) string {
	return fmt.Sprintf("%q", s)
}
