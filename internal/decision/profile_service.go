package decision

import (
	"fmt"
	"strings"
)

type ProfileService struct {
	workspaceRoot string
}

func NewProfileService(workspaceRoot string) *ProfileService {
	return &ProfileService{workspaceRoot: strings.TrimSpace(workspaceRoot)}
}

func (s *ProfileService) WorkspaceRoot() string {
	if s == nil {
		return ""
	}
	return s.workspaceRoot
}

func (s *ProfileService) Load() (*ParsedProfile, error) {
	if s == nil {
		return nil, fmt.Errorf("decision profile service is nil")
	}
	return LoadWorkspaceProfile(s.workspaceRoot)
}
