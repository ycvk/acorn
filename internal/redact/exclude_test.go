package redact

import "testing"

func TestRenderExcludesWholeCredentials(t *testing.T) {
	const token = "abcdefghijklmnopqrstuvwx"
	raw := "curl -H 'Authorization: Bearer " + token + "' https://api.example.com done"
	for _, tc := range []struct {
		name     string
		quotes   []string
		redacted bool
		want     string
	}{
		{"memory", nil, true, "curl -H 'Authorization: Bearer [REDACTED:authorization]' https://api.example.com done"},
		{"thread", nil, false, raw},
		// A quote of the redacted text that ends inside the placeholder still
		// covers the whole token in the raw thread.
		{"thread quote of placeholder", []string{"Bearer [REDACTED"}, false, "curl -H 'Authorization: " + ExcludedPlaceholder + "' https://api.example.com done"},
		// A raw quote that cuts the token excludes all of it from memory.
		{"memory raw quote cuts token", []string{"Bearer abcdef"}, true, "curl -H 'Authorization: " + ExcludedPlaceholder + "' https://api.example.com done"},
		{"memory unrelated quote", []string{"done"}, true, "curl -H 'Authorization: Bearer [REDACTED:authorization]' https://api.example.com " + ExcludedPlaceholder},
	} {
		if got := Render(raw, tc.quotes, tc.redacted); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

func TestRenderExcludesOverlappingQuotesAsOne(t *testing.T) {
	raw := "owner moved to Hangzhou in May"
	for _, quotes := range [][]string{{"moved to Hangzhou", "Hangzhou in May"}, {"Hangzhou in May", "moved to Hangzhou"}} {
		if got, want := Render(raw, quotes, false), "owner "+ExcludedPlaceholder; got != want {
			t.Errorf("Render(%q) = %q, want %q", quotes, got, want)
		}
	}
}
