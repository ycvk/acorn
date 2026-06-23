package skills

import (
	"strings"
	"testing"
)

func TestSplitFrontmatterValid(t *testing.T) {
	raw := "---\nid: test\nname: Test\n---\n# Test\n\nbody text"
	meta, body, err := splitFrontmatter(raw)
	if err != nil {
		t.Fatalf("splitFrontmatter: %v", err)
	}
	if meta.ID != "test" || meta.Name != "Test" {
		t.Errorf("meta = {ID: %q, Name: %q}, want {test, Test}", meta.ID, meta.Name)
	}
	if !strings.Contains(body, "# Test") {
		t.Errorf("body = %q, want to contain heading", body)
	}
}

func TestSplitFrontmatterNoFrontmatter(t *testing.T) {
	raw := "# Plain skill\n\nno frontmatter"
	meta, body, err := splitFrontmatter(raw)
	if err != nil {
		t.Fatalf("splitFrontmatter: %v", err)
	}
	if meta.ID != "" {
		t.Errorf("meta.ID = %q, want empty", meta.ID)
	}
	if body != raw {
		t.Errorf("body = %q, want original text", body)
	}
}

func TestSplitFrontmatterMissingClosingDelimiter(t *testing.T) {
	raw := "---\nid: test\nname: Test\n# no closing"
	_, _, err := splitFrontmatter(raw)
	if err == nil || !strings.Contains(err.Error(), "missing closing frontmatter delimiter") {
		t.Fatalf("error = %v, want missing closing delimiter", err)
	}
}

func TestSplitFrontmatterInvalidYAML(t *testing.T) {
	raw := "---\nid: [unclosed\n---\nbody"
	_, _, err := splitFrontmatter(raw)
	if err == nil || !strings.Contains(err.Error(), "invalid frontmatter") {
		t.Fatalf("error = %v, want invalid frontmatter", err)
	}
}

func TestSplitFrontmatterEmptyFrontmatter(t *testing.T) {
	raw := "---\n---\nbody"
	meta, body, err := splitFrontmatter(raw)
	if err != nil {
		t.Fatalf("splitFrontmatter: %v", err)
	}
	if meta.ID != "" {
		t.Errorf("meta.ID = %q, want empty", meta.ID)
	}
	if body != "body" {
		t.Errorf("body = %q, want 'body'", body)
	}
}

func TestParseSkillMarkdownStripsBOM(t *testing.T) {
	raw := "\uFEFF---\nid: test\n---\nbody"
	meta, _, _, _, err := parseSkillMarkdown(raw)
	if err != nil {
		t.Fatalf("parseSkillMarkdown: %v", err)
	}
	if meta.ID != "test" {
		t.Errorf("meta.ID = %q, want test (BOM should be stripped)", meta.ID)
	}
}

func TestParseSkillBodyExtractsName(t *testing.T) {
	name, instruction := parseSkillBody("# My Skill\n\ndo stuff")
	if name != "My Skill" {
		t.Errorf("name = %q, want 'My Skill'", name)
	}
	if instruction != "do stuff" {
		t.Errorf("instruction = %q, want 'do stuff'", instruction)
	}
}

func TestParseSkillBodyNoHeading(t *testing.T) {
	name, instruction := parseSkillBody("just instruction text")
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
	if instruction != "just instruction text" {
		t.Errorf("instruction = %q, want original text", instruction)
	}
}

func TestParseSkillBodySkipsLeadingBlankLines(t *testing.T) {
	name, instruction := parseSkillBody("\n\n  \n# Named\nbody")
	if name != "Named" {
		t.Errorf("name = %q, want 'Named'", name)
	}
	if !strings.Contains(instruction, "body") {
		t.Errorf("instruction = %q, want to contain 'body'", instruction)
	}
}

func TestRenderSkillMarkdownRoundTrip(t *testing.T) {
	original := frontmatter{
		ID:   "test",
		Name: "Test",
	}
	rendered, err := renderSkillMarkdown(original, "do the thing")
	if err != nil {
		t.Fatalf("renderSkillMarkdown: %v", err)
	}
	if !strings.HasPrefix(rendered, "---\n") {
		t.Error("rendered output should start with frontmatter delimiter")
	}
	meta, body, err := splitFrontmatter(rendered)
	if err != nil {
		t.Fatalf("round-trip splitFrontmatter: %v", err)
	}
	if meta.ID != "test" || meta.Name != "Test" {
		t.Errorf("round-trip meta = {ID: %q, Name: %q}, want {test, Test}", meta.ID, meta.Name)
	}
	if !strings.Contains(body, "do the thing") {
		t.Errorf("round-trip body = %q, want to contain instruction", body)
	}
}

func TestRenderSkillMarkdownBodyRaw(t *testing.T) {
	meta := frontmatter{ID: "x", Name: "X"}
	body := "## Custom Section\n\ntext"
	rendered, err := renderSkillMarkdownBody(meta, body)
	if err != nil {
		t.Fatalf("renderSkillMarkdownBody: %v", err)
	}
	if !strings.Contains(rendered, "## Custom Section") {
		t.Errorf("rendered = %q, want to contain raw body", rendered)
	}
	if !strings.HasPrefix(rendered, "---\n") {
		t.Error("rendered output should start with frontmatter delimiter")
	}
}
