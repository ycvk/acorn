package skills

import (
	"context"
	"errors"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// ServerStatus is how a host runs the MCP server of one package: Ready when
// it serves, otherwise Error says why not.
type ServerStatus struct {
	Name  string `json:"name"`
	Hash  string `json:"hash"`
	Ready bool   `json:"ready"`
	Error string `json:"error,omitempty"`
}

// ServerReporter reports the MCP servers a host runs.
type ServerReporter interface {
	MCPServers(ctx context.Context) ([]ServerStatus, error)
}

// TriggerSchedules reads the schedules the wake scheduler keeps for package
// triggers.
type TriggerSchedules interface {
	ListTriggers(ctx context.Context) ([]core.TriggerState, error)
}

// serverStatusWait bounds asking the host how its servers run; a host that
// does not answer in time leaves them unknown.
const serverStatusWait = 10 * time.Second

// Status reads how the packages of the skills in use run, for acorn doctor
// and the owner's skill pages. It starts and stops nothing: servers start
// with the next run that uses their skill, and serve schedules triggers.
type Status struct {
	library   *Library
	schedules TriggerSchedules
	host      ServerReporter
}

// NewStatus reads package status from the library, the trigger schedules
// and the host that runs the servers, nil without a workstation.
func NewStatus(library *Library, schedules TriggerSchedules, host ServerReporter) (*Status, error) {
	if library == nil || schedules == nil {
		return nil, errors.New("package status: library and trigger schedules are required")
	}
	return &Status{library: library, schedules: schedules, host: host}, nil
}

// ServerReport is how the MCP server a skill's package declares runs. State
// is ready, failed, stopped, missing_secrets, off (no workstation) or unknown
// (the workstation did not answer).
type ServerReport struct {
	Skill          string   `json:"skill"`
	Command        string   `json:"command"`
	Secrets        []string `json:"secrets,omitempty"`
	MissingSecrets []string `json:"missing_secrets,omitempty"`
	State          string   `json:"state"`
	Error          string   `json:"error,omitempty"`
}

// Servers reports the MCP server of every skill in use that declares one:
// whether its secrets are set and how the workstation runs it.
func (s *Status) Servers(ctx context.Context) ([]ServerReport, error) {
	scan, err := s.library.Scan(ctx)
	if err != nil {
		return nil, err
	}
	var specs []Spec
	for _, spec := range scan.Skills {
		if spec.MCP != nil {
			specs = append(specs, spec)
		}
	}
	if len(specs) == 0 {
		return nil, nil
	}
	running := map[string]ServerStatus{}
	var unreachable error
	if s.host != nil {
		hostCtx, cancel := context.WithTimeout(ctx, serverStatusWait)
		statuses, err := s.host.MCPServers(hostCtx)
		cancel()
		unreachable = err
		for _, status := range statuses {
			running[status.Name] = status
		}
	}
	out := make([]ServerReport, 0, len(specs))
	for _, spec := range specs {
		report := ServerReport{Skill: spec.Name, Command: spec.MCP.CommandLine(), Secrets: spec.Secrets}
		missing, err := s.library.MissingSecrets(ctx, spec)
		if err != nil {
			return nil, err
		}
		server, ok := running[spec.Name]
		switch {
		case len(missing) > 0:
			report.State, report.MissingSecrets = "missing_secrets", missing
		case s.host == nil:
			report.State = "off"
		case unreachable != nil:
			report.State, report.Error = "unknown", unreachable.Error()
		case !ok:
			report.State = "stopped"
		case server.Hash != spec.Hash():
			report.State = "stopped"
			report.Error = "the workstation runs an older revision until the next run"
		case server.Ready:
			report.State = "ready"
		default:
			report.State, report.Error = "failed", server.Error
		}
		out = append(out, report)
	}
	return out, nil
}

// TriggerReport is how a trigger of a skill in use stands. State is
// scheduled, failing, unscheduled (serve has not scheduled it yet),
// missing_secrets or off (no workstation).
type TriggerReport struct {
	Skill          string    `json:"skill"`
	Trigger        string    `json:"trigger"`
	Schedule       string    `json:"schedule"`
	Deliver        string    `json:"deliver"`
	Run            string    `json:"run"`
	State          string    `json:"state"`
	NextAt         time.Time `json:"next_at,omitzero"`
	Failures       int       `json:"failures,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
	MissingSecrets []string  `json:"missing_secrets,omitempty"`
}

// Triggers reports the triggers of the skills in use and how their
// schedules stand. Trigger names a trigger as skill/trigger.
func (s *Status) Triggers(ctx context.Context) ([]TriggerReport, error) {
	triggers, err := s.library.Triggers(ctx)
	if err != nil || len(triggers) == 0 {
		return nil, err
	}
	states, err := s.schedules.ListTriggers(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]core.TriggerState{}
	for _, state := range states {
		byID[state.Skill+"/"+state.Name] = state
	}
	out := make([]TriggerReport, 0, len(triggers))
	for _, t := range triggers {
		report := TriggerReport{Skill: t.Skill, Trigger: t.ID(), Schedule: t.Schedule, Deliver: string(t.Deliver), Run: t.Run}
		state, scheduled := byID[t.ID()]
		switch {
		case s.host == nil:
			report.State = "off"
		case len(t.Missing) > 0:
			report.State, report.MissingSecrets = "missing_secrets", t.Missing
		case !scheduled || state.Schedule != t.Schedule:
			report.State = "unscheduled"
		default:
			report.State, report.NextAt, report.Failures, report.LastError = string(state.Status), state.NextAt, state.Failures, state.LastError
			if state.Status == core.TriggerActive {
				report.State = "scheduled"
			}
		}
		out = append(out, report)
	}
	return out, nil
}
