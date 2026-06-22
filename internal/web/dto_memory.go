package web

import (
	"github.com/ycvk/acorn/internal/memorymodule"
)

type MemoryRecordDTO struct {
	Ref         string   `json:"ref"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Scope       string   `json:"scope,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Origin      string   `json:"origin,omitempty"`
	TaskPattern string   `json:"task_pattern,omitempty"`
	Path        string   `json:"path"`
	Body        string   `json:"body"`
	Created     string   `json:"created,omitempty"`
	Updated     string   `json:"updated,omitempty"`
	SourceRun   string   `json:"source_run,omitempty"`
	SourceRefs  []string `json:"source_refs,omitempty"`
}

type MemoryRecordListResponse struct {
	Items []MemoryRecordDTO `json:"items"`
}

type MemorySearchItemDTO struct {
	Ref         string   `json:"ref"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Scope       string   `json:"scope,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Origin      string   `json:"origin,omitempty"`
	TaskPattern string   `json:"task_pattern,omitempty"`
	Path        string   `json:"path"`
	Snippet     string   `json:"snippet"`
	Score       float64  `json:"score"`
	Created     string   `json:"created,omitempty"`
	Updated     string   `json:"updated,omitempty"`
	SourceRun   string   `json:"source_run,omitempty"`
	SourceRefs  []string `json:"source_refs,omitempty"`
}

type MemorySearchResponse struct {
	Items []MemorySearchItemDTO `json:"items"`
}

func memoryRecordDTOsFromDomain(records []memorymodule.Record) []MemoryRecordDTO {
	return DefaultConverter.memoryRecordDTOsFromDomain(records)
}

func memorySearchItemDTOsFromDomain(records []memorymodule.SearchItem) []MemorySearchItemDTO {
	return DefaultConverter.memorySearchItemDTOsFromDomain(records)
}
