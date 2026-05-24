package web

import "encoding/json"

// MarshalJSON implements custom JSON serialization for MessagePartDTO
// so that each kind emits only its relevant fields.
func (part MessagePartDTO) MarshalJSON() ([]byte, error) {
	switch part.Kind {
	case "text":
		return json.Marshal(struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		}{
			Kind: part.Kind,
			Text: part.Text,
		})
	case "reasoning":
		return json.Marshal(struct {
			Kind      string `json:"kind"`
			Reasoning string `json:"reasoning"`
		}{
			Kind:      part.Kind,
			Reasoning: part.Reasoning,
		})
	case "work_status":
		return json.Marshal(struct {
			Kind        string            `json:"kind"`
			Status      string            `json:"status"`
			Title       string            `json:"title"`
			Summary     string            `json:"summary"`
			DetailRunID string            `json:"detail_run_id,omitempty"`
			Action      *MessageActionDTO `json:"action,omitempty"`
		}{
			Kind:        part.Kind,
			Status:      part.Status,
			Title:       part.Title,
			Summary:     part.Summary,
			DetailRunID: part.DetailRunID,
			Action:      part.Action,
		})
	case "decision":
		return json.Marshal(struct {
			Kind             string              `json:"kind"`
			DecisionID       string              `json:"decision_id"`
			Question         string              `json:"question"`
			Status           string              `json:"status,omitempty"`
			SelectedOptionID string              `json:"selected_option_id,omitempty"`
			Answer           string              `json:"answer,omitempty"`
			Options          []DecisionOptionDTO `json:"options"`
		}{
			Kind:             part.Kind,
			DecisionID:       part.DecisionID,
			Question:         part.Question,
			Status:           part.Status,
			SelectedOptionID: part.SelectedOptionID,
			Answer:           part.Answer,
			Options:          part.Options,
		})
	case "result":
		return json.Marshal(struct {
			Kind        string   `json:"kind"`
			Title       string   `json:"title"`
			Changed     []string `json:"changed"`
			Verified    []string `json:"verified"`
			Risks       []string `json:"risks"`
			DetailRunID string   `json:"detail_run_id,omitempty"`
		}{
			Kind:        part.Kind,
			Title:       part.Title,
			Changed:     nonNilStrings(part.Changed),
			Verified:    nonNilStrings(part.Verified),
			Risks:       nonNilStrings(part.Risks),
			DetailRunID: part.DetailRunID,
		})
	case "disclosure":
		return json.Marshal(struct {
			Kind  string              `json:"kind"`
			Items []DisclosureItemDTO `json:"items"`
		}{
			Kind:  part.Kind,
			Items: part.Items,
		})
	case "technical_detail_link":
		return json.Marshal(struct {
			Kind        string `json:"kind"`
			DetailRunID string `json:"detail_run_id,omitempty"`
			RunID       string `json:"run_id"`
			Label       string `json:"label"`
		}{
			Kind:        part.Kind,
			DetailRunID: part.DetailRunID,
			RunID:       part.RunID,
			Label:       part.Label,
		})
	default:
		type alias MessagePartDTO
		return json.Marshal(alias(part))
	}
}
