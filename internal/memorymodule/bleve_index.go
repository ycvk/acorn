//go:build bleve_faiss && vectors && cgo

package memorymodule

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	bleveSemanticFieldKind         = "kind"
	bleveSemanticFieldScope        = "scope"
	bleveSemanticFieldScopeEmpty   = "scope_empty"
	bleveSemanticFieldStatus       = "status"
	bleveSemanticFieldOrigin       = "origin"
	bleveSemanticFieldTitle        = "title"
	bleveSemanticFieldBody         = "body"
	bleveSemanticFieldPath         = "path"
	bleveSemanticFieldTags         = "tags"
	bleveSemanticFieldTaskPattern  = "task_pattern"
	bleveSemanticFieldSourceRun    = "source_run"
	bleveSemanticFieldSourceRefs   = "source_refs_json"
	bleveSemanticFieldEvidenceRefs = "evidence_refs_json"
	bleveSemanticFieldRelations    = "relations_json"
	bleveSemanticFieldContentHash  = "content_hash"
	bleveSemanticFieldCreated      = "created"
	bleveSemanticFieldUpdated      = "updated"
	bleveSemanticFieldValidFrom    = "valid_from"
	bleveSemanticFieldValidUntil   = "valid_until"
	bleveSemanticFieldModel        = "model"
	bleveSemanticFieldDimensions   = "dimensions"
	bleveSemanticFieldSchema       = "schema"
	bleveSemanticFieldVector       = "vector"

	bleveSemanticSearchHeadroom = 8
)

type bleveSemanticIndex struct {
	path       string
	indexName  string
	indexPath  string
	dimensions int
}

type bleveSemanticDocument struct {
	Kind             string    `json:"kind"`
	Scope            string    `json:"scope"`
	ScopeEmpty       bool      `json:"scope_empty"`
	Status           string    `json:"status"`
	Origin           string    `json:"origin"`
	Title            string    `json:"title"`
	Body             string    `json:"body"`
	Path             string    `json:"path"`
	Tags             []string  `json:"tags"`
	TaskPattern      string    `json:"task_pattern"`
	SourceRun        string    `json:"source_run"`
	SourceRefsJSON   string    `json:"source_refs_json"`
	EvidenceRefsJSON string    `json:"evidence_refs_json"`
	RelationsJSON    string    `json:"relations_json"`
	ContentHash      string    `json:"content_hash"`
	Created          string    `json:"created"`
	Updated          string    `json:"updated"`
	ValidFrom        string    `json:"valid_from"`
	ValidUntil       string    `json:"valid_until"`
	Model            string    `json:"model"`
	Dimensions       int       `json:"dimensions"`
	Schema           string    `json:"schema"`
	Vector           []float32 `json:"vector"`
}

func NewBleveSemanticIndex(ctx context.Context, cfg BleveSemanticIndexConfig) (SemanticIndex, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		return nil, errors.New("bleve semantic index path is required")
	}
	indexName := strings.TrimSpace(cfg.IndexName)
	if indexName == "" {
		return nil, errors.New("bleve semantic index index name is required")
	}
	if cfg.Dimensions <= 0 {
		return nil, errors.New("bleve semantic index dimensions must be > 0")
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, fmt.Errorf("create bleve semantic index directory %q: %w", path, err)
	}
	return &bleveSemanticIndex{
		path:       path,
		indexName:  indexName,
		indexPath:  filepath.Join(path, indexName),
		dimensions: cfg.Dimensions,
	}, nil
}

func (i *bleveSemanticIndex) Close() error {
	return nil
}
