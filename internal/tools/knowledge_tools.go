package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
)

// KnowledgeVault is the knowledge base as the knowledge tools use it.
type KnowledgeVault interface {
	Write(ctx context.Context, note knowledge.WriteNote) (core.KnowledgeNote, error)
	Edit(ctx context.Context, path, old, replacement, runID string) (core.KnowledgeNote, error)
	Read(ctx context.Context, path string) (core.KnowledgeNote, error)
	Search(ctx context.Context, query string, limit int) ([]core.KnowledgeHit, error)
	Recent(ctx context.Context, prefix string, limit int) ([]core.KnowledgeHit, error)
}

// KnowledgeToolDeps are what the knowledge tools need.
type KnowledgeToolDeps struct {
	Vault    KnowledgeVault
	Context  core.ToolCallContextBridge
	Location *time.Location
}

const (
	defaultKnowledgeLimit = 10
	maxKnowledgeLimit     = 50
)

// KnowledgeWriteInput is the input of knowledge_write.
type KnowledgeWriteInput struct {
	Path   string   `json:"path" jsonschema_description:"Note path relative to the knowledge base, ending in .md, e.g. inbox/rust-async.md. Folders are created as needed."`
	Title  string   `json:"title" jsonschema_description:"Note title."`
	Body   string   `json:"body" jsonschema_description:"The whole markdown body."`
	Tags   []string `json:"tags,omitempty" jsonschema_description:"Optional tags."`
	Source string   `json:"source,omitempty" jsonschema_description:"Optional source URL the note is based on."`
}

// KnowledgeEditInput is the input of knowledge_edit.
type KnowledgeEditInput struct {
	Path string `json:"path" jsonschema_description:"Path of an existing note."`
	Old  string `json:"old" jsonschema_description:"Exact text in the body to replace; it must occur exactly once."`
	New  string `json:"new" jsonschema_description:"Replacement text."`
}

// KnowledgeWriteOutput reports the stored revision of a note.
type KnowledgeWriteOutput struct {
	Path     string `json:"path"`
	Title    string `json:"title"`
	Revision int64  `json:"revision"`
	Updated  string `json:"updated"`
}

// KnowledgeReadInput is the input of knowledge_read.
type KnowledgeReadInput struct {
	Path string `json:"path" jsonschema_description:"Note path relative to the knowledge base."`
}

// KnowledgeReadOutput is one note.
type KnowledgeReadOutput struct {
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	Tags    []string `json:"tags,omitempty"`
	Source  string   `json:"source,omitempty"`
	Created string   `json:"created"`
	Updated string   `json:"updated"`
	Body    string   `json:"body"`
}

// KnowledgeSearchInput is the input of knowledge_search.
type KnowledgeSearchInput struct {
	Query string `json:"query" jsonschema_description:"Words to look for in titles, tags and bodies."`
	Limit int    `json:"limit,omitempty" jsonschema_description:"Maximum results, 1-50. Defaults to 10."`
}

// KnowledgeListInput is the input of knowledge_list.
type KnowledgeListInput struct {
	Prefix string `json:"prefix,omitempty" jsonschema_description:"Only notes whose path starts with this, e.g. inbox/."`
	Limit  int    `json:"limit,omitempty" jsonschema_description:"Maximum results, 1-50. Defaults to 10."`
}

// KnowledgeHitOutput is one note in a search or listing.
type KnowledgeHitOutput struct {
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	Tags    []string `json:"tags,omitempty"`
	Snippet string   `json:"snippet"`
	Updated string   `json:"updated"`
}

// KnowledgeHitsOutput is a search or listing result.
type KnowledgeHitsOutput struct {
	Notes []KnowledgeHitOutput `json:"notes"`
}

func buildKnowledgeWriteTool(deps KnowledgeToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("knowledge_write",
		"Create a note in the knowledge base, or replace a note's whole body. Every write is kept as a new revision.",
		func(ctx context.Context, input KnowledgeWriteInput, _ ToolProgressEmitter) (KnowledgeWriteOutput, error) {
			note, err := deps.Vault.Write(ctx, knowledge.WriteNote{
				Path:   input.Path,
				Title:  input.Title,
				Tags:   input.Tags,
				Source: input.Source,
				Body:   input.Body,
				RunID:  deps.Context.CurrentRunID(ctx),
			})
			if err != nil {
				return KnowledgeWriteOutput{}, fmt.Errorf("knowledge_write: %w", err)
			}
			return writeOutput(note, deps.Location), nil
		})
}

func buildKnowledgeEditTool(deps KnowledgeToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("knowledge_edit",
		"Change part of an existing note by replacing one exact piece of its body. Use it to add to or correct a note without rewriting it.",
		func(ctx context.Context, input KnowledgeEditInput, _ ToolProgressEmitter) (KnowledgeWriteOutput, error) {
			note, err := deps.Vault.Edit(ctx, input.Path, input.Old, input.New, deps.Context.CurrentRunID(ctx))
			if err != nil {
				return KnowledgeWriteOutput{}, fmt.Errorf("knowledge_edit: %w", err)
			}
			return writeOutput(note, deps.Location), nil
		})
}

func buildKnowledgeReadTool(deps KnowledgeToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("knowledge_read", "Read one note from the knowledge base.",
		func(ctx context.Context, input KnowledgeReadInput, _ ToolProgressEmitter) (KnowledgeReadOutput, error) {
			note, err := deps.Vault.Read(ctx, input.Path)
			if err != nil {
				return KnowledgeReadOutput{}, fmt.Errorf("knowledge_read: %w", err)
			}
			return KnowledgeReadOutput{
				Path:    note.Path,
				Title:   note.Title,
				Tags:    note.Tags,
				Source:  note.Source,
				Created: note.CreatedAt.In(deps.Location).Format(localTimeLayout),
				Updated: note.UpdatedAt.In(deps.Location).Format(localTimeLayout),
				Body:    note.Body,
			}, nil
		})
}

func buildKnowledgeSearchTool(deps KnowledgeToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("knowledge_search",
		"Search the knowledge base. Search before writing a new note so related notes get extended instead of duplicated.",
		func(ctx context.Context, input KnowledgeSearchInput, _ ToolProgressEmitter) (KnowledgeHitsOutput, error) {
			query := strings.TrimSpace(input.Query)
			if query == "" {
				return KnowledgeHitsOutput{}, errors.New("knowledge_search: query is required")
			}
			limit, err := knowledgeLimit(input.Limit)
			if err != nil {
				return KnowledgeHitsOutput{}, fmt.Errorf("knowledge_search: %w", err)
			}
			hits, err := deps.Vault.Search(ctx, query, limit)
			if err != nil {
				return KnowledgeHitsOutput{}, fmt.Errorf("knowledge_search: %w", err)
			}
			return hitsOutput(hits, deps.Location), nil
		})
}

func buildKnowledgeListTool(deps KnowledgeToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("knowledge_list",
		"List notes in the knowledge base, most recently updated first, to see how it is organized.",
		func(ctx context.Context, input KnowledgeListInput, _ ToolProgressEmitter) (KnowledgeHitsOutput, error) {
			limit, err := knowledgeLimit(input.Limit)
			if err != nil {
				return KnowledgeHitsOutput{}, fmt.Errorf("knowledge_list: %w", err)
			}
			hits, err := deps.Vault.Recent(ctx, strings.TrimSpace(input.Prefix), limit)
			if err != nil {
				return KnowledgeHitsOutput{}, fmt.Errorf("knowledge_list: %w", err)
			}
			return hitsOutput(hits, deps.Location), nil
		})
}

func knowledgeLimit(limit int) (int, error) {
	switch {
	case limit == 0:
		return defaultKnowledgeLimit, nil
	case limit < 0 || limit > maxKnowledgeLimit:
		return 0, fmt.Errorf("limit must be between 1 and %d", maxKnowledgeLimit)
	default:
		return limit, nil
	}
}

func writeOutput(note core.KnowledgeNote, loc *time.Location) KnowledgeWriteOutput {
	return KnowledgeWriteOutput{
		Path:     note.Path,
		Title:    note.Title,
		Revision: note.Revision,
		Updated:  note.UpdatedAt.In(loc).Format(localTimeLayout),
	}
}

func hitsOutput(hits []core.KnowledgeHit, loc *time.Location) KnowledgeHitsOutput {
	out := KnowledgeHitsOutput{Notes: make([]KnowledgeHitOutput, 0, len(hits))}
	for _, hit := range hits {
		out.Notes = append(out.Notes, KnowledgeHitOutput{
			Path:    hit.Path,
			Title:   hit.Title,
			Tags:    hit.Tags,
			Snippet: hit.Snippet,
			Updated: hit.UpdatedAt.In(loc).Format(localTimeLayout),
		})
	}
	return out
}
