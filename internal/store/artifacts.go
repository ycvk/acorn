package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type ArtifactKind string

const (
	ArtifactKindText       ArtifactKind = "text"
	ArtifactKindMarkdown   ArtifactKind = "markdown"
	ArtifactKindJSON       ArtifactKind = "json"
	ArtifactKindDiff       ArtifactKind = "diff"
	ArtifactKindLog        ArtifactKind = "log"
	ArtifactKindTestReport ArtifactKind = "test_report"
	ArtifactKindBinary     ArtifactKind = "binary"
)

// ArtifactDB is the persistence contract for artifact metadata.
// *Store implements it in production; tests use in-memory mocks.
type ArtifactDB interface {
	SaveArtifact(context.Context, core.ArtifactRecord) (core.ArtifactRecord, error)
	LoadArtifact(context.Context, string) (core.ArtifactRecord, error)
	ListByRun(context.Context, string) ([]core.ArtifactRecord, error)
	ListBySession(context.Context, string) ([]core.ArtifactRecord, error)
}

type ArtifactService struct {
	rootDir string
	store   ArtifactDB
}

var _ core.ArtifactService = (*ArtifactService)(nil)

func NewArtifactService(rootDir string, store ArtifactDB) (*ArtifactService, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("artifact root dir is required")
	}
	if store == nil {
		return nil, fmt.Errorf("artifact store is required")
	}
	cleanRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact root dir: %w", err)
	}
	if err := os.MkdirAll(cleanRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create artifact root dir: %w", err)
	}
	return &ArtifactService{rootDir: cleanRoot, store: store}, nil
}

// WriteArtifact implements core.ArtifactService.
func (s *ArtifactService) WriteArtifact(ctx context.Context, req core.ArtifactWriteRequest) (core.ArtifactRecord, error) {
	if s == nil {
		return core.ArtifactRecord{}, fmt.Errorf("artifact service is nil")
	}
	if err := ctx.Err(); err != nil {
		return core.ArtifactRecord{}, err
	}
	req, err := NormalizeArtifactWriteRequest(req)
	if err != nil {
		return core.ArtifactRecord{}, err
	}
	if req.ArtifactID == "" {
		req.ArtifactID, err = generateArtifactID()
		if err != nil {
			return core.ArtifactRecord{}, err
		}
	}
	sum := sha256.Sum256(req.Content)
	record := core.ArtifactRecord{
		ArtifactID:          req.ArtifactID,
		RunID:               req.RunID,
		SessionID:           req.SessionID,
		SourceToolResultRef: req.SourceToolResultRef,
		Kind:                req.Kind,
		Title:               req.Title,
		MIMEType:            req.MIMEType,
		RelativePath:        relativePathFor(req.RunID, req.ArtifactID),
		SizeBytes:           int64(len(req.Content)),
		SHA256:              hex.EncodeToString(sum[:]),
		CreatedAt:           req.CreatedAt,
	}
	record, err = NormalizeArtifactRecord(record)
	if err != nil {
		return core.ArtifactRecord{}, err
	}
	artifactPath, err := s.pathFor(record.RelativePath)
	if err != nil {
		return core.ArtifactRecord{}, err
	}
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		return core.ArtifactRecord{}, fmt.Errorf("create artifact content dir: %w", err)
	}
	tmpPath := artifactPath + ".tmp-" + record.ArtifactID
	if err := os.WriteFile(tmpPath, req.Content, 0o600); err != nil {
		return core.ArtifactRecord{}, fmt.Errorf("write artifact content temp file: %w", err)
	}
	if err := os.Rename(tmpPath, artifactPath); err != nil {
		_ = os.Remove(tmpPath)
		return core.ArtifactRecord{}, fmt.Errorf("commit artifact content file: %w", err)
	}
	saved, err := s.store.SaveArtifact(ctx, record)
	if err != nil {
		removeErr := os.Remove(artifactPath)
		if removeErr != nil {
			return core.ArtifactRecord{}, errors.Join(fmt.Errorf("save artifact metadata: %w", err), fmt.Errorf("remove untracked artifact content: %w", removeErr))
		}
		return core.ArtifactRecord{}, fmt.Errorf("save artifact metadata: %w", err)
	}
	return saved, nil
}

// ReadArtifactRange implements core.ArtifactService.
func (s *ArtifactService) ReadArtifactRange(ctx context.Context, req core.ArtifactReadRangeRequest) (core.ArtifactReadRangeResult, error) {
	if s == nil {
		return core.ArtifactReadRangeResult{}, fmt.Errorf("artifact service is nil")
	}
	if err := ctx.Err(); err != nil {
		return core.ArtifactReadRangeResult{}, err
	}
	req.ArtifactID = strings.TrimSpace(req.ArtifactID)
	if req.ArtifactID == "" {
		return core.ArtifactReadRangeResult{}, fmt.Errorf("artifact_id is required")
	}
	if req.Offset < 0 {
		return core.ArtifactReadRangeResult{}, fmt.Errorf("artifact range offset must be >= 0")
	}
	if req.Limit <= 0 {
		return core.ArtifactReadRangeResult{}, fmt.Errorf("artifact range limit must be > 0")
	}
	record, err := s.store.LoadArtifact(ctx, req.ArtifactID)
	if err != nil {
		return core.ArtifactReadRangeResult{}, err
	}
	artifactPath, err := s.pathFor(record.RelativePath)
	if err != nil {
		return core.ArtifactReadRangeResult{}, err
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		return core.ArtifactReadRangeResult{}, fmt.Errorf("stat artifact content %s: %w", record.ArtifactID, err)
	}
	if info.Size() != record.SizeBytes {
		return core.ArtifactReadRangeResult{}, fmt.Errorf("artifact content size mismatch for %s: got %d want %d", record.ArtifactID, info.Size(), record.SizeBytes)
	}
	if req.Offset > record.SizeBytes {
		return core.ArtifactReadRangeResult{}, fmt.Errorf("artifact range offset %d exceeds size %d", req.Offset, record.SizeBytes)
	}
	file, err := os.Open(artifactPath)
	if err != nil {
		return core.ArtifactReadRangeResult{}, fmt.Errorf("open artifact content %s: %w", record.ArtifactID, err)
	}
	defer file.Close()
	if _, err := file.Seek(req.Offset, io.SeekStart); err != nil {
		return core.ArtifactReadRangeResult{}, fmt.Errorf("seek artifact content %s: %w", record.ArtifactID, err)
	}
	content, err := io.ReadAll(io.LimitReader(file, req.Limit))
	if err != nil {
		return core.ArtifactReadRangeResult{}, fmt.Errorf("read artifact content %s: %w", record.ArtifactID, err)
	}
	return core.ArtifactReadRangeResult{
		Record:  record,
		Offset:  req.Offset,
		Content: content,
		EOF:     req.Offset+int64(len(content)) >= record.SizeBytes,
	}, nil
}

func (s *ArtifactService) ListByRun(ctx context.Context, runID string) ([]core.ArtifactRecord, error) {
	return s.store.ListByRun(ctx, runID)
}

func (s *ArtifactService) ListBySession(ctx context.Context, sessionID string) ([]core.ArtifactRecord, error) {
	return s.store.ListBySession(ctx, sessionID)
}

func NormalizeArtifactWriteRequest(req core.ArtifactWriteRequest) (core.ArtifactWriteRequest, error) {
	req.ArtifactID = strings.TrimSpace(req.ArtifactID)
	req.RunID = strings.TrimSpace(req.RunID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.SourceToolResultRef = strings.TrimSpace(req.SourceToolResultRef)
	req.Kind = strings.TrimSpace(req.Kind)
	req.Title = strings.TrimSpace(req.Title)
	req.MIMEType = strings.TrimSpace(req.MIMEType)
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	} else {
		req.CreatedAt = req.CreatedAt.UTC()
	}
	if req.ArtifactID != "" {
		if err := validateOpaqueID("artifact_id", req.ArtifactID); err != nil {
			return core.ArtifactWriteRequest{}, err
		}
	}
	if req.RunID == "" {
		return core.ArtifactWriteRequest{}, fmt.Errorf("artifact run_id is required")
	}
	if err := validateArtifactKind(ArtifactKind(req.Kind)); err != nil {
		return core.ArtifactWriteRequest{}, err
	}
	return req, nil
}

func NormalizeArtifactRecord(record core.ArtifactRecord) (core.ArtifactRecord, error) {
	record.ArtifactID = strings.TrimSpace(record.ArtifactID)
	record.RunID = strings.TrimSpace(record.RunID)
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.SourceToolResultRef = strings.TrimSpace(record.SourceToolResultRef)
	record.Kind = strings.TrimSpace(record.Kind)
	record.Title = strings.TrimSpace(record.Title)
	record.MIMEType = strings.TrimSpace(record.MIMEType)
	record.RelativePath = filepath.ToSlash(strings.TrimSpace(record.RelativePath))
	record.SHA256 = strings.ToLower(strings.TrimSpace(record.SHA256))
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	} else {
		record.CreatedAt = record.CreatedAt.UTC()
	}
	if err := validateOpaqueID("artifact_id", record.ArtifactID); err != nil {
		return core.ArtifactRecord{}, err
	}
	if record.RunID == "" {
		return core.ArtifactRecord{}, fmt.Errorf("artifact run_id is required")
	}
	if err := validateArtifactKind(ArtifactKind(record.Kind)); err != nil {
		return core.ArtifactRecord{}, err
	}
	if record.RelativePath == "" {
		return core.ArtifactRecord{}, fmt.Errorf("artifact relative_path is required")
	}
	if err := validateRelativePath(record.RelativePath); err != nil {
		return core.ArtifactRecord{}, err
	}
	if record.SizeBytes < 0 {
		return core.ArtifactRecord{}, fmt.Errorf("artifact size_bytes must be >= 0")
	}
	if len(record.SHA256) != sha256.Size*2 {
		return core.ArtifactRecord{}, fmt.Errorf("artifact sha256 must be %d hex characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(record.SHA256); err != nil {
		return core.ArtifactRecord{}, fmt.Errorf("artifact sha256 is invalid: %w", err)
	}
	return record, nil
}

func (s *ArtifactService) pathFor(relativePath string) (string, error) {
	if err := validateRelativePath(relativePath); err != nil {
		return "", err
	}
	fullPath := filepath.Join(s.rootDir, filepath.FromSlash(relativePath))
	cleanRoot := filepath.Clean(s.rootDir)
	cleanFull := filepath.Clean(fullPath)
	rel, err := filepath.Rel(cleanRoot, cleanFull)
	if err != nil {
		return "", fmt.Errorf("resolve artifact relative path: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("artifact path escapes root: %s", relativePath)
	}
	return cleanFull, nil
}

func validateArtifactKind(kind ArtifactKind) error {
	switch kind {
	case ArtifactKindText, ArtifactKindMarkdown, ArtifactKindJSON, ArtifactKindDiff, ArtifactKindLog, ArtifactKindTestReport, ArtifactKindBinary:
		return nil
	default:
		return fmt.Errorf("artifact kind %q is invalid", kind)
	}
}

func validateOpaqueID(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if value == "." || value == ".." || strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("%s must be an opaque identifier", field)
	}
	return nil
}

func validateRelativePath(value string) error {
	if filepath.IsAbs(value) {
		return fmt.Errorf("artifact relative_path must not be absolute")
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("artifact relative_path must stay under artifact root")
	}
	return nil
}

func relativePathFor(runID string, artifactID string) string {
	hash := sha256.Sum256([]byte(runID))
	return filepath.ToSlash(filepath.Join("runs", hex.EncodeToString(hash[:8]), artifactID))
}

func generateArtifactID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate artifact id: %w", err)
	}
	return "artifact_" + hex.EncodeToString(raw[:]), nil
}
