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

var ErrArtifactNotFound = errors.New("artifact not found")

type ArtifactRecord struct {
	ArtifactID          string
	RunID               string
	SessionID           string
	SourceToolResultRef string
	Kind                ArtifactKind
	Title               string
	MIMEType            string
	RelativePath        string
	SizeBytes           int64
	SHA256              string
	CreatedAt           time.Time
}

type ArtifactWriteRequest struct {
	ArtifactID          string
	RunID               string
	SessionID           string
	SourceToolResultRef string
	Kind                ArtifactKind
	Title               string
	MIMEType            string
	Content             []byte
	CreatedAt           time.Time
}

type ArtifactReadRangeRequest struct {
	ArtifactID string
	Offset     int64
	Limit      int64
}

type ArtifactReadRangeResult struct {
	Record  ArtifactRecord
	Offset  int64
	Content []byte
	EOF     bool
}

type ArtifactStore interface {
	SaveArtifact(context.Context, ArtifactRecord) (ArtifactRecord, error)
	LoadArtifact(context.Context, string) (ArtifactRecord, error)
	ListArtifactsByRun(context.Context, string) ([]ArtifactRecord, error)
	ListArtifactsBySession(context.Context, string) ([]ArtifactRecord, error)
}

type ArtifactService struct {
	rootDir string
	store   ArtifactStore
}

func NewArtifactService(rootDir string, store ArtifactStore) (*ArtifactService, error) {
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

func (s *ArtifactService) Write(ctx context.Context, req ArtifactWriteRequest) (ArtifactRecord, error) {
	if s == nil {
		return ArtifactRecord{}, fmt.Errorf("artifact service is nil")
	}
	if err := ctx.Err(); err != nil {
		return ArtifactRecord{}, err
	}
	req, err := NormalizeArtifactWriteRequest(req)
	if err != nil {
		return ArtifactRecord{}, err
	}
	if req.ArtifactID == "" {
		req.ArtifactID, err = generateArtifactID()
		if err != nil {
			return ArtifactRecord{}, err
		}
	}
	sum := sha256.Sum256(req.Content)
	record := ArtifactRecord{
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
		return ArtifactRecord{}, err
	}
	artifactPath, err := s.pathFor(record.RelativePath)
	if err != nil {
		return ArtifactRecord{}, err
	}
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		return ArtifactRecord{}, fmt.Errorf("create artifact content dir: %w", err)
	}
	tmpPath := artifactPath + ".tmp-" + record.ArtifactID
	if err := os.WriteFile(tmpPath, req.Content, 0o600); err != nil {
		return ArtifactRecord{}, fmt.Errorf("write artifact content temp file: %w", err)
	}
	if err := os.Rename(tmpPath, artifactPath); err != nil {
		_ = os.Remove(tmpPath)
		return ArtifactRecord{}, fmt.Errorf("commit artifact content file: %w", err)
	}
	saved, err := s.store.SaveArtifact(ctx, record)
	if err != nil {
		removeErr := os.Remove(artifactPath)
		if removeErr != nil {
			return ArtifactRecord{}, errors.Join(fmt.Errorf("save artifact metadata: %w", err), fmt.Errorf("remove untracked artifact content: %w", removeErr))
		}
		return ArtifactRecord{}, fmt.Errorf("save artifact metadata: %w", err)
	}
	return saved, nil
}

func (s *ArtifactService) ReadRange(ctx context.Context, req ArtifactReadRangeRequest) (ArtifactReadRangeResult, error) {
	if s == nil {
		return ArtifactReadRangeResult{}, fmt.Errorf("artifact service is nil")
	}
	if err := ctx.Err(); err != nil {
		return ArtifactReadRangeResult{}, err
	}
	req.ArtifactID = strings.TrimSpace(req.ArtifactID)
	if req.ArtifactID == "" {
		return ArtifactReadRangeResult{}, fmt.Errorf("artifact_id is required")
	}
	if req.Offset < 0 {
		return ArtifactReadRangeResult{}, fmt.Errorf("artifact range offset must be >= 0")
	}
	if req.Limit <= 0 {
		return ArtifactReadRangeResult{}, fmt.Errorf("artifact range limit must be > 0")
	}
	record, err := s.store.LoadArtifact(ctx, req.ArtifactID)
	if err != nil {
		return ArtifactReadRangeResult{}, err
	}
	artifactPath, err := s.pathFor(record.RelativePath)
	if err != nil {
		return ArtifactReadRangeResult{}, err
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		return ArtifactReadRangeResult{}, fmt.Errorf("stat artifact content %s: %w", record.ArtifactID, err)
	}
	if info.Size() != record.SizeBytes {
		return ArtifactReadRangeResult{}, fmt.Errorf("artifact content size mismatch for %s: got %d want %d", record.ArtifactID, info.Size(), record.SizeBytes)
	}
	if req.Offset > record.SizeBytes {
		return ArtifactReadRangeResult{}, fmt.Errorf("artifact range offset %d exceeds size %d", req.Offset, record.SizeBytes)
	}
	file, err := os.Open(artifactPath)
	if err != nil {
		return ArtifactReadRangeResult{}, fmt.Errorf("open artifact content %s: %w", record.ArtifactID, err)
	}
	defer file.Close()
	if _, err := file.Seek(req.Offset, io.SeekStart); err != nil {
		return ArtifactReadRangeResult{}, fmt.Errorf("seek artifact content %s: %w", record.ArtifactID, err)
	}
	content, err := io.ReadAll(io.LimitReader(file, req.Limit))
	if err != nil {
		return ArtifactReadRangeResult{}, fmt.Errorf("read artifact content %s: %w", record.ArtifactID, err)
	}
	return ArtifactReadRangeResult{
		Record:  record,
		Offset:  req.Offset,
		Content: content,
		EOF:     req.Offset+int64(len(content)) >= record.SizeBytes,
	}, nil
}

func (s *ArtifactService) Verify(ctx context.Context, artifactID string) (ArtifactRecord, error) {
	if s == nil {
		return ArtifactRecord{}, fmt.Errorf("artifact service is nil")
	}
	if err := ctx.Err(); err != nil {
		return ArtifactRecord{}, err
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return ArtifactRecord{}, fmt.Errorf("artifact_id is required")
	}
	record, err := s.store.LoadArtifact(ctx, artifactID)
	if err != nil {
		return ArtifactRecord{}, err
	}
	artifactPath, err := s.pathFor(record.RelativePath)
	if err != nil {
		return ArtifactRecord{}, err
	}
	file, err := os.Open(artifactPath)
	if err != nil {
		return ArtifactRecord{}, fmt.Errorf("open artifact content %s: %w", record.ArtifactID, err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return ArtifactRecord{}, fmt.Errorf("hash artifact content %s: %w", record.ArtifactID, err)
	}
	if size != record.SizeBytes {
		return ArtifactRecord{}, fmt.Errorf("artifact content size mismatch for %s: got %d want %d", record.ArtifactID, size, record.SizeBytes)
	}
	gotHash := hex.EncodeToString(hash.Sum(nil))
	if gotHash != record.SHA256 {
		return ArtifactRecord{}, fmt.Errorf("artifact content sha256 mismatch for %s: got %s want %s", record.ArtifactID, gotHash, record.SHA256)
	}
	return record, nil
}

func (s *ArtifactService) ListByRun(ctx context.Context, runID string) ([]ArtifactRecord, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("artifact run_id is required")
	}
	return s.store.ListArtifactsByRun(ctx, runID)
}

func (s *ArtifactService) ListBySession(ctx context.Context, sessionID string) ([]ArtifactRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("artifact session_id is required")
	}
	return s.store.ListArtifactsBySession(ctx, sessionID)
}

func NormalizeArtifactWriteRequest(req ArtifactWriteRequest) (ArtifactWriteRequest, error) {
	req.ArtifactID = strings.TrimSpace(req.ArtifactID)
	req.RunID = strings.TrimSpace(req.RunID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.SourceToolResultRef = strings.TrimSpace(req.SourceToolResultRef)
	req.Kind = ArtifactKind(strings.TrimSpace(string(req.Kind)))
	req.Title = strings.TrimSpace(req.Title)
	req.MIMEType = strings.TrimSpace(req.MIMEType)
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	} else {
		req.CreatedAt = req.CreatedAt.UTC()
	}
	if req.ArtifactID != "" {
		if err := validateOpaqueID("artifact_id", req.ArtifactID); err != nil {
			return ArtifactWriteRequest{}, err
		}
	}
	if req.RunID == "" {
		return ArtifactWriteRequest{}, fmt.Errorf("artifact run_id is required")
	}
	if err := validateArtifactKind(req.Kind); err != nil {
		return ArtifactWriteRequest{}, err
	}
	return req, nil
}

func NormalizeArtifactRecord(record ArtifactRecord) (ArtifactRecord, error) {
	record.ArtifactID = strings.TrimSpace(record.ArtifactID)
	record.RunID = strings.TrimSpace(record.RunID)
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.SourceToolResultRef = strings.TrimSpace(record.SourceToolResultRef)
	record.Kind = ArtifactKind(strings.TrimSpace(string(record.Kind)))
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
		return ArtifactRecord{}, err
	}
	if record.RunID == "" {
		return ArtifactRecord{}, fmt.Errorf("artifact run_id is required")
	}
	if err := validateArtifactKind(record.Kind); err != nil {
		return ArtifactRecord{}, err
	}
	if record.RelativePath == "" {
		return ArtifactRecord{}, fmt.Errorf("artifact relative_path is required")
	}
	if err := validateRelativePath(record.RelativePath); err != nil {
		return ArtifactRecord{}, err
	}
	if record.SizeBytes < 0 {
		return ArtifactRecord{}, fmt.Errorf("artifact size_bytes must be >= 0")
	}
	if len(record.SHA256) != sha256.Size*2 {
		return ArtifactRecord{}, fmt.Errorf("artifact sha256 must be %d hex characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(record.SHA256); err != nil {
		return ArtifactRecord{}, fmt.Errorf("artifact sha256 is invalid: %w", err)
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
