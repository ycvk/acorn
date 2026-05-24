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
	ValidFrom    string                    `json:"valid_from,omitempty"`
	ValidUntil   string                    `json:"valid_until,omitempty"`
	SourceRun    string                    `json:"source_run,omitempty"`
	SourceRefs   []string                  `json:"source_refs,omitempty"`
	EvidenceRefs []string                  `json:"evidence_refs,omitempty"`
	Relations    []MemoryRecordRelationDTO `json:"relations,omitempty"`
}

type MemoryRecordRelationDTO struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Reason string `json:"reason,omitempty"`
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
	ValidFrom    string                    `json:"valid_from,omitempty"`
	ValidUntil   string                    `json:"valid_until,omitempty"`
	SourceRun    string                    `json:"source_run,omitempty"`
	SourceRefs   []string                  `json:"source_refs,omitempty"`
	EvidenceRefs []string                  `json:"evidence_refs,omitempty"`
	Relations    []MemoryRecordRelationDTO `json:"relations,omitempty"`
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
	if view == nil {
		return nil
	}
	return &WorkingCheckpointDTO{
		ThreadID:       view.ThreadID,
		Content:        view.Content,
		RelatedSkillID: view.RelatedSkillID,
		UpdatedAt:      view.UpdatedAt,
	}
}

func memoryRecordDTOsFromDomain(records []memorymodule.Record) []MemoryRecordDTO {
	items := make([]MemoryRecordDTO, 0, len(records))
	for _, record := range records {
		items = append(items, MemoryRecordDTO{
			Ref:          record.Ref,
			Kind:         string(record.Kind),
			Title:        record.Title,
			Status:       string(record.Status),
			Scope:        record.Scope,
			Tags:         append([]string(nil), record.Tags...),
			Origin:       record.Origin,
			TaskPattern:  record.TaskPattern,
			Path:         record.RelPath,
			Body:         record.Body,
			Created:      record.Created,
			Updated:      record.Updated,
			ValidFrom:    record.ValidFrom,
			ValidUntil:   record.ValidUntil,
			SourceRun:    record.SourceRun,
			SourceRefs:   append([]string(nil), record.SourceRefs...),
			EvidenceRefs: append([]string(nil), record.EvidenceRefs...),
			Relations:    memoryRecordRelationDTOsFromDomain(record.Relations),
		})
	}
	return items
}

func memorySearchItemDTOsFromDomain(records []memorymodule.SearchItem) []MemorySearchItemDTO {
	items := make([]MemorySearchItemDTO, 0, len(records))
	for _, record := range records {
		items = append(items, MemorySearchItemDTO{
			Ref:          record.Ref,
			Kind:         record.Kind,
			Title:        record.Title,
			Status:       record.Status,
			Scope:        record.Scope,
			Tags:         append([]string(nil), record.Tags...),
			Origin:       record.Origin,
			TaskPattern:  record.TaskPattern,
			Path:         record.Path,
			Snippet:      record.Snippet,
			Score:        record.Score,
			Created:      record.Created,
			Updated:      record.Updated,
			ValidFrom:    record.ValidFrom,
			ValidUntil:   record.ValidUntil,
			SourceRun:    record.SourceRun,
			SourceRefs:   append([]string(nil), record.SourceRefs...),
			EvidenceRefs: append([]string(nil), record.EvidenceRefs...),
			Relations:    memoryRecordRelationDTOsFromDomain(record.Relations),
		})
	}
	return items
}

func memoryRecordRelationDTOsFromDomain(items []memorymodule.RecordRelation) []MemoryRecordRelationDTO {
	out := make([]MemoryRecordRelationDTO, 0, len(items))
	for _, item := range items {
		out = append(out, MemoryRecordRelationDTO{
			Type:   string(item.Type),
			Target: item.Target,
			Reason: item.Reason,
		})
	}
	return out
}
