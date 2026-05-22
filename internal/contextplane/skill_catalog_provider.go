package contextplane

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/skills"
)

func buildSkillCatalogMessage(snapshot *skills.Snapshot) *schema.Message {
	brief := skillCatalogBrief(snapshot)
	if strings.TrimSpace(brief) == "" {
		return nil
	}
	return buildContextEnvelopeMessage("skill-catalog", brief)
}

func skillCatalogBrief(snapshot *skills.Snapshot) string {
	if snapshot == nil || len(snapshot.Skills) == 0 {
		return ""
	}
	views := make([]skills.View, 0, len(snapshot.Skills))
	for _, item := range snapshot.Skills {
		if item.LifecycleStatus == skills.LifecycleRetired {
			continue
		}
		views = append(views, skills.CopyView(item))
	}
	if len(views) == 0 {
		return ""
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].ID != views[j].ID {
			return views[i].ID < views[j].ID
		}
		return views[i].Name < views[j].Name
	})

	lines := []string{
		"Available skills are runtime capabilities, not user input.",
		"When the user asks what you can do or which skill fits, inspect this catalog first and prefer the matching skill over a generic answer.",
		"If the catalog summary is not enough, call skill_list or skill_view before answering.",
		"Catalog:",
	}
	for _, view := range views {
		status := "eligible"
		if !view.Eligible {
			status = "ineligible"
		}
		label := fmt.Sprintf("- %s (%s) [%s]", view.ID, view.Name, status)
		if summary := strings.TrimSpace(view.Summary); summary != "" {
			label += ": " + summary
		}
		lines = append(lines, label)
		details := make([]string, 0, 3)
		if len(view.TriggerHints) > 0 {
			limit := min(2, len(view.TriggerHints))
			details = append(details, "triggers="+strings.Join(view.TriggerHints[:limit], ", "))
		}
		if len(view.Requires.Tools) > 0 {
			details = append(details, "tools="+strings.Join(view.Requires.Tools, ", "))
		}
		if len(view.DisabledReasons) > 0 {
			details = append(details, "disabled="+strings.Join(view.DisabledReasons, "; "))
		}
		if len(details) > 0 {
			lines = append(lines, "  "+strings.Join(details, " | "))
		}
	}
	if len(snapshot.Problems) > 0 {
		lines = append(lines, "Catalog problems:")
		for _, problem := range snapshot.Problems {
			label := "- " + strings.TrimSpace(problem.Error)
			if strings.TrimSpace(problem.ID) != "" {
				label = fmt.Sprintf("- %s (%s)", problem.ID, strings.TrimSpace(problem.Error))
			}
			lines = append(lines, label)
		}
	}
	return strings.Join(lines, "\n")
}
