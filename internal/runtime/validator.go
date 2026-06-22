package runtime

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

type validationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type toolArgumentValidator struct {
	toolName string
	schema   *jsonschema.Schema
}

func newToolArgumentValidator(toolName string, params any) (*toolArgumentValidator, error) {
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

	return &toolArgumentValidator{
		toolName: toolName,
		schema:   compiled,
	}, nil
}

func newToolArgumentValidatorFromToolInfo(info *schema.ToolInfo) (*toolArgumentValidator, error) {
	if info == nil {
		return nil, errors.New("nil ToolInfo")
	}
	js, err := info.ToJSONSchema()
	if err != nil {
		return nil, fmt.Errorf("convert ToolInfo to JSONSchema for %q: %w", info.Name, err)
	}
	if js == nil {
		return &toolArgumentValidator{toolName: info.Name, schema: nil}, nil
	}
	schemaBytes, err := json.Marshal(js)
	if err != nil {
		return nil, fmt.Errorf("marshal JSONSchema for %q: %w", info.Name, err)
	}
	var params any
	if err := json.Unmarshal(schemaBytes, &params); err != nil {
		return nil, fmt.Errorf("unmarshal JSONSchema for %q: %w", info.Name, err)
	}
	return newToolArgumentValidator(info.Name, params)
}

func (v *toolArgumentValidator) validate(argumentsJSON string) ([]validationError, error) {
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

func formatValidationError(toolName string, errors []validationError) string {
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

func flattenValidationErrors(verr *jsonschema.ValidationError) []validationError {
	var out []validationError
	collectErrors(verr, &out)
	return out
}

func collectErrors(verr *jsonschema.ValidationError, out *[]validationError) {
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
			*out = append(*out, validationError{
				Field:   path,
				Message: defaultEnglishPrinter.Sprintf("missing required field %s", quoteJSONSchema(missing)),
			})
		}
		return
	}

	*out = append(*out, validationError{
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
