package decision

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProfileService provides access to the workspace decision profile.
type ProfileService struct {
	workspaceRoot string
}

// NewProfileService creates a new profile service for the given workspace root.
func NewProfileService(workspaceRoot string) *ProfileService {
	return &ProfileService{workspaceRoot: strings.TrimSpace(workspaceRoot)}
}

// WorkspaceRoot returns the configured workspace root.
func (s *ProfileService) WorkspaceRoot() string {
	if s == nil {
		return ""
	}
	return s.workspaceRoot
}

// Load loads and parses the decision profile for the workspace.
func (s *ProfileService) Load() (*ParsedProfile, error) {
	if s == nil {
		return nil, fmt.Errorf("decision profile service is nil")
	}
	return LoadWorkspaceProfile(s.workspaceRoot)
}

const BlockDefaults = "acorn-defaults"

type ParsedProfile struct {
	Profile Profile
	Raw     string
	Hash    string
	Path    string
}

func DefaultProfile() Profile {
	return Profile{
		Defaults: Defaults{
			MissingContext:            ActionInspectFirst,
			MissingRequiredCapability: ActionBlock,
		},
	}
}

func LoadWorkspaceProfile(workspaceRoot string) (*ParsedProfile, error) {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		raw, err := RenderProfileMarkdown(DefaultProfile())
		if err != nil {
			return nil, err
		}
		hash := profileHash(raw)
		return &ParsedProfile{Profile: DefaultProfile(), Raw: raw, Hash: hash}, nil
	}
	path := filepath.Join(root, "decision.md")
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			raw, err := RenderProfileMarkdown(DefaultProfile())
			if err != nil {
				return nil, err
			}
			hash := profileHash(raw)
			return &ParsedProfile{Profile: DefaultProfile(), Raw: raw, Hash: hash, Path: path}, nil
		}
		return nil, fmt.Errorf("read decision.md: %w", err)
	}
	profile, err := ParseProfileMarkdown(string(body))
	if err != nil {
		return nil, err
	}
	return &ParsedProfile{
		Profile: profile,
		Raw:     string(body),
		Hash:    profileHash(string(body)),
		Path:    path,
	}, nil
}

func ParseProfileMarkdown(raw string) (Profile, error) {
	profile := DefaultProfile()
	if block, ok := extractBlock(raw, BlockDefaults); ok {
		if err := yaml.Unmarshal([]byte(block), &profile.Defaults); err != nil {
			return Profile{}, fmt.Errorf("parse %s: %w", BlockDefaults, err)
		}
	}
	if err := validateProfile(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func RenderProfileMarkdown(profile Profile) (string, error) {
	defaults, err := yaml.Marshal(profile.Defaults)
	if err != nil {
		return "", fmt.Errorf("marshal defaults: %w", err)
	}
	raw := strings.Join([]string{
		"# Acorn Decision Profile",
		"",
		"This file is the canonical workspace decision contract for Acorn runs.",
		"Only the active fenced blocks below are runtime-active.",
		"",
		"## Defaults",
		"",
		fmt.Sprintf("```%s\n%s```", BlockDefaults, defaults),
	}, "\n")
	return strings.TrimSpace(raw) + "\n", nil
}

func validateProfile(profile Profile) error {
	validAction := map[string]struct{}{
		ActionExecuteWithSkill:    {},
		ActionInspectFirst:        {},
		ActionExecuteWithoutSkill: {},
		ActionAskUser:             {},
		ActionBlock:               {},
	}
	for _, action := range []string{
		profile.Defaults.MissingContext,
		profile.Defaults.MissingRequiredCapability,
	} {
		if _, ok := validAction[action]; !ok {
			return fmt.Errorf("invalid decision action %q", action)
		}
	}
	return nil
}

func profileHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func extractBlock(raw, name string) (string, bool) {
	re := regexp.MustCompile("(?ms)^```" + regexp.QuoteMeta(name) + "\\n(.*?)^```\\s*$")
	matches := re.FindStringSubmatch(raw)
	if len(matches) != 2 {
		return "", false
	}
	return strings.TrimSpace(matches[1]), true
}
