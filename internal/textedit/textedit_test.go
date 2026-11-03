package textedit

import (
	"slices"
	"strings"
	"testing"
)

// Edits replace the one place each old text occurs, all against the text as
// it is, and report the line each new text starts on.
func TestApplyReplacesEachOldTextOnce(t *testing.T) {
	text := "name = acorn\nport = 8080\nmode = dev\n"
	got, lines, err := Apply(text, []Edit{
		{Old: "mode = dev", New: "mode = prod\nlog = info"},
		{Old: "port = 8080", New: "port = 9090"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "name = acorn\nport = 9090\nmode = prod\nlog = info\n" || !slices.Equal(lines, []int{3, 2}) {
		t.Fatalf("got %q, lines %v", got, lines)
	}
}

// Old text that does not occur exactly is matched with whitespace at line
// ends and typographic punctuation folded, and the file's own text is
// replaced.
func TestApplyMatchesLooselyWhenNothingMatchesExactly(t *testing.T) {
	text := "def greet():  \n    print(\u201Chello\u201D)\t\n    return \u2014 1\n"
	got, lines, err := Apply(text, []Edit{{Old: "def greet():\n    print(\"hello\")", New: "def greet():\n    print(\"hi\")"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "def greet():\n    print(\"hi\")\t\n    return \u2014 1\n" || !slices.Equal(lines, []int{1}) {
		t.Fatalf("got %q, lines %v", got, lines)
	}
	got, _, err = Apply("价格\u00A0100 元\n", []Edit{{Old: "价格 100 元", New: "价格 120 元"}})
	if err != nil || got != "价格 120 元\n" {
		t.Fatalf("non-breaking space: got %q, %v", got, err)
	}
}

// CRLF line endings and a byte order mark stay as the file had them.
func TestApplyKeepsLineEndingsAndByteOrderMark(t *testing.T) {
	text := "\uFEFFfirst\r\nsecond\r\nthird\r\n"
	got, lines, err := Apply(text, []Edit{{Old: "second\nthird", New: "2\n3"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "\uFEFFfirst\r\n2\r\n3\r\n" || !slices.Equal(lines, []int{2}) {
		t.Fatalf("got %q, lines %v", got, lines)
	}
}

// An edit that cannot be placed exactly once fails every edit.
func TestApplyRefusesEditsItCannotPlace(t *testing.T) {
	text := "a = 1\nb = 1\nc = 2\n"
	for _, tc := range []struct {
		edits []Edit
		want  string
	}{
		{nil, "at least one edit"},
		{[]Edit{{Old: "", New: "x"}}, "edit 1: old text is empty"},
		{[]Edit{{Old: "c = 2", New: "c = 2"}}, "edit 1: old and new text are the same"},
		{[]Edit{{Old: "c = 2", New: "c = 3"}, {Old: "= 1", New: "= 0"}}, "edit 2: the old text occurs 2 times"},
		{[]Edit{{Old: "d = 4", New: "d = 5"}}, "edit 1: the old text is not in the file"},
		{[]Edit{{Old: "a = 1\nb", New: "x"}, {Old: "b = 1", New: "y"}}, "edits 1 and 2 overlap"},
	} {
		got, _, err := Apply(text, tc.edits)
		if err == nil || !strings.Contains(err.Error(), tc.want) || got != "" {
			t.Fatalf("edits %+v: got %q, err %v, want %q", tc.edits, got, err, tc.want)
		}
	}
}
