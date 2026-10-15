// Package redact finds credentials in text that memory must never retain.
// The rules only cover high-confidence shapes: vendor key formats, labelled
// header and password values, private key blocks, JWTs, credentialed URLs and
// digits explicitly labelled as a verification code.
package redact

import (
	"regexp"
	"sort"
	"strings"
)

// Span is a credential at text[Start:End].
type Span struct {
	Start, End int
	Kind       string
}

type rule struct {
	kind string
	re   *regexp.Regexp
	// group selects the submatch that holds the credential; 0 is the whole match.
	group int
}

// secretValue is a printable ASCII value without quotes, separators or a
// leading placeholder such as ${VAR} or <token>.
const secretValue = `([A-Za-z0-9!@#%^&*()_+=.?/~|\[\]{}:-][A-Za-z0-9!@#$%^&*()_+=.?/~|\[\]{}<>:-]{3,})`

var rules = []rule{
	{kind: "private_key", re: regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----(?:[\s\S]*?-----END [A-Z0-9 ]*PRIVATE KEY-----|[\s\S]*)`)},
	{kind: "anthropic_key", re: regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`)},
	{kind: "openai_key", re: regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{kind: "stripe_key", re: regexp.MustCompile(`\b[rs]k_(?:live|test)_[A-Za-z0-9]{16,}`)},
	{kind: "github_token", re: regexp.MustCompile(`\b(?:gh[oprsu]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})`)},
	{kind: "slack_token", re: regexp.MustCompile(`\bxox[abeoprs]-[A-Za-z0-9-]{10,}`)},
	{kind: "slack_webhook", re: regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9/_-]+`)},
	{kind: "google_key", re: regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{35}`)},
	{kind: "google_token", re: regexp.MustCompile(`\bya29\.[A-Za-z0-9_-]{20,}`)},
	{kind: "aws_key", re: regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
	{kind: "aws_secret", re: regexp.MustCompile(`(?i)\baws_secret_access_key["']?[ \t]*[:=][ \t]*["']?([A-Za-z0-9/+=]{40})`), group: 1},
	{kind: "jwt", re: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)},
	{kind: "authorization", re: regexp.MustCompile(`(?i)\b(?:proxy-)?authorization["']?[ \t]*[:=][ \t]*["']?(?:(?:basic|bearer|token)[ \t]+)?([A-Za-z0-9._~+/=-]{8,})`), group: 1},
	{kind: "bearer_token", re: regexp.MustCompile(`(?i)\bbearer[ \t]+([A-Za-z0-9._~+/=-]{16,})`), group: 1},
	{kind: "cookie", re: regexp.MustCompile(`(?im)^[ \t]*(?:set-)?cookie[ \t]*:[ \t]*([^\r\n]+)`), group: 1},
	{kind: "password", re: regexp.MustCompile(`\b[a-z][a-z0-9+.-]*://[^\s:/@]*:([^\s@/]+)@`), group: 1},
	{kind: "password", re: regexp.MustCompile(`(?i)\b(?:password|passwd)["']?[ \t]*[:=][ \t]*["']?` + secretValue), group: 1},
	{kind: "password", re: regexp.MustCompile(`密码[ \t]*(?:[:：=]|是|为)[ \t]*` + secretValue), group: 1},
	{kind: "otp", re: regexp.MustCompile(`(?i)(?:\b(?:verification|security|confirmation|login|sign-in|authentication|one-time)[ \t]+code|\b(?:otp|2fa|mfa|passcode)\b)[^0-9\n]{0,16}?\b([0-9]{4,8})\b`), group: 1},
	{kind: "otp", re: regexp.MustCompile(`(?:验证码|校验码|动态码|动态密码|确认码|安全码|一次性密码)[^0-9\n]{0,12}?([0-9]{4,8})\b`), group: 1},
}

// Find returns the credentials in text in order, without overlaps; where two
// rules overlap, the earlier and then longer match wins.
func Find(text string) []Span {
	var found []Span
	for _, r := range rules {
		for _, m := range r.re.FindAllStringSubmatchIndex(text, -1) {
			start, end := m[2*r.group], m[2*r.group+1]
			if start < end {
				found = append(found, Span{Start: start, End: end, Kind: r.kind})
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Start != found[j].Start {
			return found[i].Start < found[j].Start
		}
		return found[i].End > found[j].End
	})
	out := found[:0]
	end := 0
	for _, s := range found {
		if s.Start >= end {
			out = append(out, s)
			end = s.End
		}
	}
	return out
}

// Placeholder is the text that replaces a credential of kind.
func Placeholder(kind string) string { return "[REDACTED:" + kind + "]" }

// Text replaces every credential in text with its placeholder.
func Text(text string) string {
	spans := Find(text)
	if len(spans) == 0 {
		return text
	}
	var b strings.Builder
	at := 0
	for _, s := range spans {
		b.WriteString(text[at:s.Start])
		b.WriteString(Placeholder(s.Kind))
		at = s.End
	}
	b.WriteString(text[at:])
	return b.String()
}
