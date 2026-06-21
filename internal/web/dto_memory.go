package web

import (
	"time"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/memorymodule"
)

type MemoryRecordDTO struct {
	Ref          string                    `json:"ref"`
	Kind         string                    `json:"kind"`
	Title        string                    `json:"title"`
	Status       string                    `json:"status"`
	Scope        string                    `json:"scope,omitempty"`
	Tags         []string                  `json:"tags,omitempty"`
	Origin       string                    `json:"origin,omitempty"`
	TaskPattern  string                    `json:"task_pattern,omitempty"`
	Path         string                    `json:"path"`
	Body         string                    `json:"body"`
	Created      string                    `json:"created,omitempty"`
	Updated      string                    `json:"updated,omitempty"`
	SourceRun    string                    `json:"source_run,omitempty"`
	SourceRefs   []string                  `json:"source_refs,omitempty"`
}


type MemoryRecordListResponse struct {
	Items []MemoryRecordDTO `json:"items"`
}

type MemorySearchItemDTO struct {
	Ref          string                    `json:"ref"`
	Kind         string                    `json:"kind"`
	Title        string                    `json:"title"`
	Status       string                    `json:"status"`
	Scope        string                    `json:"scope,omitempty"`
	Tags         []string                  `json:"tags,omitempty"`
	Origin       string                    `json:"origin,omitempty"`
	TaskPattern  string                    `json:"task_pattern,omitempty"`
	Path         string                    `json:"path"`
	Snippet      string                    `json:"snippet"`
	Score        float64                   `json:"score"`
	Created      string                    `json:"created,omitempty"`
	Updated      string                    `json:"updated,omitempty"`
	SourceRun    string                    `json:"source_run,omitempty"`
	SourceRefs   []string                  `json:"source_refs,omitempty"`
}

type MemorySearchResponse struct {
	Items []MemorySearchItemDTO `json:"items"`
}

type WorkingCheckpointEnvelope struct {
	Item *WorkingCheckpointDTO `json:"item,omitempty"`
}

type UpdateWorkingCheckpointRequest struct {
	Content        string `json:"content"`
	RelatedSkillID string `json:"related_skill_id"`
}

type WorkingCheckpointDTO struct {
	ThreadID       string    `json:"thread_id"`
	Content        string    `json:"content"`
	RelatedSkillID string    `json:"related_skill_id"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func workingCheckpointDTOFromView(view *app.WorkingCheckpointView) *WorkingCheckpointDTO {
	return DefaultConverter.workingCheckpointDTOFromView(view)
}

func memoryRecordDTOsFromDomain(records []memorymodule.Record) []MemoryRecordDTO {
	return DefaultConverter.memoryRecordDTOsFromDomain(records)
}

func memorySearchItemDTOsFromDomain(records []memorymodule.SearchItem) []MemorySearchItemDTO {
	return DefaultConverter.memorySearchItemDTOsFromDomain(records)
}
