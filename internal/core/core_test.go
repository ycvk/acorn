package core

import (
	"reflect"
	"strings"
	"testing"
)

// --- ToolSpec.Validate() tests ---

func TestToolSpecValidate(t *testing.T) {
	valid := ToolSpec{
		ToolContract: ToolContract{
			Name:      "test_tool",
			Kind:      ToolKindNative,
			Category:  ToolCategoryRead,
			Loading:   EagerLoadingPolicy(),
			Execution: ToolExecutionPolicy{ParallelPolicy: ParallelPolicyReadOnly},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid spec failed: %v", err)
	}

	cases := []struct {
		name string
		spec ToolSpec
		err  string
	}{
		{
			name: "missing_name",
			spec: ToolSpec{ToolContract: ToolContract{
				Kind: ToolKindNative, Category: ToolCategoryRead,
				Loading:   EagerLoadingPolicy(),
				Execution: ToolExecutionPolicy{ParallelPolicy: ParallelPolicyReadOnly},
			}},
			err: "name is required",
		},
		{
			name: "missing_kind",
			spec: ToolSpec{ToolContract: ToolContract{
				Name: "x", Category: ToolCategoryRead,
				Loading:   EagerLoadingPolicy(),
				Execution: ToolExecutionPolicy{ParallelPolicy: ParallelPolicyReadOnly},
			}},
			err: "kind is required",
		},
		{
			name: "missing_category",
			spec: ToolSpec{ToolContract: ToolContract{
				Name: "x", Kind: ToolKindNative,
				Loading:   EagerLoadingPolicy(),
				Execution: ToolExecutionPolicy{ParallelPolicy: ParallelPolicyReadOnly},
			}},
			err: "category is required",
		},
		{
			name: "missing_loading_mode",
			spec: ToolSpec{ToolContract: ToolContract{
				Name: "x", Kind: ToolKindNative, Category: ToolCategoryRead,
				Execution: ToolExecutionPolicy{ParallelPolicy: ParallelPolicyReadOnly},
			}},
			err: "loading mode is required",
		},
		{
			name: "invalid_parallel_policy",
			spec: ToolSpec{ToolContract: ToolContract{
				Name: "x", Kind: ToolKindNative, Category: ToolCategoryRead,
				Loading:   EagerLoadingPolicy(),
				Execution: ToolExecutionPolicy{ParallelPolicy: "bogus"},
			}},
			err: "unknown tool parallel policy",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.err)
			}
			if !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("expected error containing %q, got %q", tc.err, err.Error())
			}
		})
	}
}

// --- Store interface compile-time checks ---

func TestStoreInterfaces(t *testing.T) {
	cases := []struct {
		name        string
		iface       any
		wantMethods int
	}{
		{"SessionStore", (*SessionStore)(nil), 31},
		{"IdentityStore", (*IdentityStore)(nil), 7},
		{"ArtifactStore", (*ArtifactStore)(nil), 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typ := reflect.TypeOf(tc.iface).Elem()
			if got := typ.NumMethod(); got != tc.wantMethods {
				t.Fatalf("%s has %d methods, want %d", tc.name, got, tc.wantMethods)
			}
		})
	}
}

// --- Adapted from domain/stream_accessors_test.go ---

func TestAccessors(t *testing.T) {
	cases := []struct {
		name          string
		item          StreamItem
		wantMessage   bool
		wantDelta     bool
		wantInterrupt bool
	}{
		{
			name:        "assistant_message",
			item:        StreamItem{Payload: map[string]any{"message": &StreamMessage{Role: "assistant", Content: "hi"}}},
			wantMessage: true,
		},
		{
			name:        "run_completed",
			item:        StreamItem{Payload: map[string]any{"message": &StreamMessage{Role: "assistant", Content: "done"}}},
			wantMessage: true,
		},
		{
			name:      "assistant_delta",
			item:      StreamItem{Payload: map[string]any{"assistant_delta": &StreamAssistantDelta{Delta: "delta"}}},
			wantDelta: true,
		},
		{
			name:          "run_interrupted",
			item:          StreamItem{Payload: map[string]any{"interrupt": &StreamInterrupt{ContextCount: 1}}},
			wantInterrupt: true,
		},
		{
			name: "no_payload",
			item: StreamItem{Payload: nil},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ItemGetMessage(tc.item); (got != nil) != tc.wantMessage {
				t.Fatalf("GetMessage() = %v, want %v", got != nil, tc.wantMessage)
			}
			if got := ItemGetAssistantDelta(tc.item); (got != nil) != tc.wantDelta {
				t.Fatalf("GetAssistantDelta() = %v, want %v", got != nil, tc.wantDelta)
			}
			if got := ItemGetInterrupt(tc.item); (got != nil) != tc.wantInterrupt {
				t.Fatalf("GetInterrupt() = %v, want %v", got != nil, tc.wantInterrupt)
			}
		})
	}
}

func TestCompactInterruptInfo(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want map[string]any
	}{
		{
			name: "valid_keys",
			in:   map[string]any{"kind": "action", "message": "hello", "extra": "ignored"},
			want: map[string]any{"kind": "action", "message": "hello"},
		},
		{
			name: "non_map",
			in:   "string",
			want: nil,
		},
		{
			name: "empty_after_filter",
			in:   map[string]any{"extra": "ignored"},
			want: nil,
		},
		{
			name: "nil",
			in:   nil,
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CompactInterruptInfo(tc.in)
			if got == nil && tc.want == nil {
				return
			}
			gm, ok := got.(map[string]any)
			if !ok {
				t.Fatalf("got non-map: %v", got)
			}
			if len(gm) != len(tc.want) {
				t.Fatalf("len = %d, want %d", len(gm), len(tc.want))
			}
			for k, v := range tc.want {
				if gm[k] != v {
					t.Fatalf("%s = %v, want %v", k, gm[k], v)
				}
			}
		})
	}
}
