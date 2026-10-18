package api

import (
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

// maxShownFileBytes bounds the files whose content a skill page carries.
const maxShownFileBytes = 64 << 10

// SkillSummaryDTO is one stored skill in the owner's list.
type SkillSummaryDTO struct {
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	Revision        int64     `json:"revision"`
	Author          string    `json:"author"`
	UpdatedAt       time.Time `json:"updated_at"`
	Retired         bool      `json:"retired"`
	Eligible        bool      `json:"eligible"`
	DisabledReasons []string  `json:"disabled_reasons,omitempty"`
	FileCount       int       `json:"file_count"`
	HasManifest     bool      `json:"has_manifest"`
	Problem         string    `json:"problem,omitempty"`
}

type SkillListResponse struct {
	Items []SkillSummaryDTO `json:"items"`
}

// SkillFileDTO is a file a skill carries; Content is set for UTF-8 text of
// at most maxShownFileBytes.
type SkillFileDTO struct {
	Path    string  `json:"path"`
	Size    int64   `json:"size"`
	Content *string `json:"content,omitempty"`
}

type SkillPackageServerDTO struct {
	CommandLine string `json:"command_line"`
	State       string `json:"state,omitempty"`
	Error       string `json:"error,omitempty"`
}

type SkillPackageTriggerDTO struct {
	Name      string    `json:"name"`
	Schedule  string    `json:"schedule"`
	Deliver   string    `json:"deliver"`
	Run       string    `json:"run"`
	State     string    `json:"state,omitempty"`
	NextAt    time.Time `json:"next_at,omitzero"`
	Failures  int       `json:"failures,omitempty"`
	LastError string    `json:"last_error,omitempty"`
}

type SkillSecretDTO struct {
	Name string `json:"name"`
	Set  bool   `json:"set"`
}

type SkillPackageDTO struct {
	MCP      *SkillPackageServerDTO   `json:"mcp,omitempty"`
	Triggers []SkillPackageTriggerDTO `json:"triggers"`
	Secrets  []SkillSecretDTO         `json:"secrets"`
}

// SkillDetailDTO is a skill's page: its summary, the content of its current
// revision and the run that wrote it, and its package.
type SkillDetailDTO struct {
	SkillSummaryDTO
	RunID       string           `json:"run_id,omitempty"`
	SHA256      string           `json:"sha256"`
	Markdown    string           `json:"markdown"`
	Instruction string           `json:"instruction,omitempty"`
	Files       []SkillFileDTO   `json:"files"`
	Package     *SkillPackageDTO `json:"package,omitempty"`
}

type SkillEnvelope struct {
	Item SkillDetailDTO `json:"item"`
}

// SkillRevisionSummaryDTO is one revision in a skill's history. SHA256
// names the package it holds.
type SkillRevisionSummaryDTO struct {
	Revision int64     `json:"revision"`
	Author   string    `json:"author"`
	RunID    string    `json:"run_id,omitempty"`
	At       time.Time `json:"at"`
	Retired  bool      `json:"retired"`
	SHA256   string    `json:"sha256"`
}

type SkillRevisionListResponse struct {
	Revisions []SkillRevisionSummaryDTO `json:"revisions"`
}

// SkillRevisionDTO is one revision of a skill with its content.
type SkillRevisionDTO struct {
	SkillRevisionSummaryDTO
	Name        string         `json:"name"`
	Markdown    string         `json:"markdown"`
	Instruction string         `json:"instruction,omitempty"`
	Files       []SkillFileDTO `json:"files"`
	Problem     string         `json:"problem,omitempty"`
}

type SkillDiffLineDTO struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type SkillDiffHunkDTO struct {
	OldStart int                `json:"old_start"`
	NewStart int                `json:"new_start"`
	Lines    []SkillDiffLineDTO `json:"lines"`
}

type SkillFileDiffDTO struct {
	Path   string             `json:"path"`
	Change string             `json:"change"`
	Binary bool               `json:"binary"`
	Hunks  []SkillDiffHunkDTO `json:"hunks"`
}

type SkillDiffDTO struct {
	Name  string             `json:"name"`
	From  int64              `json:"from"`
	To    int64              `json:"to"`
	Files []SkillFileDiffDTO `json:"files"`
}

type SkillSecretListResponse struct {
	Secrets []SkillSecretDTO `json:"secrets"`
}

// RollbackSkillRequest is POST /v1/skills/{name}:rollback.
type RollbackSkillRequest struct {
	Revision     int64 `json:"revision"`
	BaseRevision int64 `json:"base_revision"`
}

// SkillChangeRequest is POST /v1/skills/{name}:retire and :restore.
type SkillChangeRequest struct {
	BaseRevision int64 `json:"base_revision"`
}

// SetSkillSecretRequest is PUT /v1/skills/{name}/secrets/{secret}.
type SetSkillSecretRequest struct {
	Value string `json:"value"`
}

func skillSummaryDTO(o SkillOverview) SkillSummaryDTO {
	skill := o.Entry.Skill
	return SkillSummaryDTO{
		Name:            skill.ID,
		Description:     o.Entry.Spec.Description,
		Revision:        skill.Revision,
		Author:          string(skill.Author),
		UpdatedAt:       skill.UpdatedAt,
		Retired:         skill.Retired,
		Eligible:        o.Eligible,
		DisabledReasons: append([]string(nil), o.Reasons...),
		FileCount:       len(skill.Files),
		HasManifest:     skills.HasManifest(skill.Files),
		Problem:         o.Entry.Problem,
	}
}

func skillSummaryDTOs(items []SkillOverview) []SkillSummaryDTO {
	out := make([]SkillSummaryDTO, 0, len(items))
	for _, item := range items {
		out = append(out, skillSummaryDTO(item))
	}
	return out
}

func skillFileDTOs(files []core.SkillFile) []SkillFileDTO {
	out := make([]SkillFileDTO, 0, len(files))
	for _, f := range files {
		dto := SkillFileDTO{Path: f.Path, Size: int64(len(f.Content))}
		if len(f.Content) <= maxShownFileBytes && skills.IsText(f.Content) {
			dto.Content = new(string(f.Content))
		}
		out = append(out, dto)
	}
	return out
}

func skillDetailDTO(page SkillPage) SkillDetailDTO {
	current := page.Current
	return SkillDetailDTO{
		SkillSummaryDTO: skillSummaryDTO(page.Overview),
		RunID:           current.Revision.RunID,
		SHA256:          current.Hash(),
		Markdown:        current.Revision.Markdown,
		Instruction:     current.Spec.Instruction,
		Files:           skillFileDTOs(current.Files),
		Package:         skillPackageDTO(page.Package),
	}
}

func skillPackageDTO(state *SkillPackageState) *SkillPackageDTO {
	if state == nil {
		return nil
	}
	out := &SkillPackageDTO{Triggers: []SkillPackageTriggerDTO{}, Secrets: skillSecretDTOs(state.Secrets)}
	if state.Spec.MCP != nil {
		out.MCP = &SkillPackageServerDTO{CommandLine: state.Spec.MCP.CommandLine()}
		if state.Server != nil {
			out.MCP.State, out.MCP.Error = state.Server.State, state.Server.Error
		}
	}
	for _, t := range state.Spec.Triggers {
		dto := SkillPackageTriggerDTO{Name: t.Name, Schedule: t.Schedule, Deliver: string(t.Deliver), Run: t.Run}
		for _, report := range state.Triggers {
			if report.Trigger == state.Spec.Name+"/"+t.Name {
				dto.State, dto.NextAt, dto.Failures, dto.LastError = report.State, report.NextAt, report.Failures, report.LastError
			}
		}
		out.Triggers = append(out.Triggers, dto)
	}
	return out
}

func skillSecretDTOs(states []SkillSecretState) []SkillSecretDTO {
	out := make([]SkillSecretDTO, 0, len(states))
	for _, s := range states {
		out = append(out, SkillSecretDTO(s))
	}
	return out
}

func skillRevisionSummaryDTO(r core.SkillRevision) SkillRevisionSummaryDTO {
	return SkillRevisionSummaryDTO{Revision: r.Revision, Author: string(r.Author), RunID: r.RunID, At: r.At, Retired: r.Retired, SHA256: skills.RevisionHash(r)}
}

func skillRevisionListResponse(revisions []core.SkillRevision) SkillRevisionListResponse {
	out := SkillRevisionListResponse{Revisions: make([]SkillRevisionSummaryDTO, 0, len(revisions))}
	for _, r := range revisions {
		out.Revisions = append(out.Revisions, skillRevisionSummaryDTO(r))
	}
	return out
}

func skillRevisionDTO(v skills.Version) SkillRevisionDTO {
	return SkillRevisionDTO{
		SkillRevisionSummaryDTO: skillRevisionSummaryDTO(v.Revision),
		Name:                    v.Revision.ID,
		Markdown:                v.Revision.Markdown,
		Instruction:             v.Spec.Instruction,
		Files:                   skillFileDTOs(v.Files),
		Problem:                 v.Problem,
	}
}

func skillDiffDTO(name string, from, to int64, files []skills.FileDiff) SkillDiffDTO {
	out := SkillDiffDTO{Name: name, From: from, To: to, Files: make([]SkillFileDiffDTO, 0, len(files))}
	for _, f := range files {
		file := SkillFileDiffDTO{Path: f.Path, Change: string(f.Change), Binary: f.Binary, Hunks: make([]SkillDiffHunkDTO, 0, len(f.Hunks))}
		for _, h := range f.Hunks {
			hunk := SkillDiffHunkDTO{OldStart: h.OldStart, NewStart: h.NewStart, Lines: make([]SkillDiffLineDTO, 0, len(h.Lines))}
			for _, l := range h.Lines {
				hunk.Lines = append(hunk.Lines, SkillDiffLineDTO{Kind: string(l.Kind), Text: l.Text})
			}
			file.Hunks = append(file.Hunks, hunk)
		}
		out.Files = append(out.Files, file)
	}
	return out
}
