package review

import (
	"path"
	"strings"
)

// Shell is a shell command split into the parts review cares about.
type Shell struct {
	// Opaque is set when the command hides what it runs, through command or
	// process substitution, or quoting that does not close.
	Opaque bool
	// ReadOnly is set when every simple command is known to only read or to
	// stay on the workstation, so the command needs no review.
	ReadOnly bool
	// Other is set when a part that is neither read-only nor a bsk page
	// action needs review as a whole command.
	Other bool
	// Bsk holds every bsk command, in order.
	Bsk []BrowserCall
}

// Actions are the bsk commands that act on a page.
func (s Shell) Actions() []BrowserCall {
	var out []BrowserCall
	for _, c := range s.Bsk {
		if bskActions[c.Action] {
			out = append(out, c)
		}
	}
	return out
}

// BrowserCall is one bsk command: its subcommand, the session it targets and,
// for page actions, the ref and value.
type BrowserCall struct {
	Action  string
	Ref     string
	Value   string
	Session string
}

// readOnlyPrograms only read or work on the workstation's own files.
var readOnlyPrograms = map[string]bool{
	"basename": true, "cat": true, "cd": true, "cut": true, "date": true, "dirname": true,
	"echo": true, "egrep": true, "grep": true, "head": true, "jq": true, "ls": true,
	"printf": true, "pwd": true, "sleep": true, "sort": true, "tail": true, "test": true,
	"tr": true, "true": true, "uniq": true, "wc": true, "[": true,
}

// bskReadOnly are bsk subcommands that read the page, move around in it or
// hand a step to the owner, without submitting anything.
var bskReadOnly = map[string]bool{
	"blur": true, "browsers": true, "console": true, "doctor": true, "download": true,
	"emulate": true, "focus": true, "get-html": true, "help": true, "hover": true,
	"logs": true, "navigate": true, "navigate-back": true, "navigate-forward": true,
	"network": true, "observe": true, "record": true, "reload": true, "request-help": true,
	"screenshot": true, "scroll-to": true, "session": true, "snapshot": true, "status": true,
	"tab": true, "wait-for-navigation": true, "wait-ms": true, "wheel": true, "window": true,
}

// bskActions submit input to the page.
var bskActions = map[string]bool{"click": true, "fill": true, "press": true, "select": true}

// ParseShell splits a command into simple commands and classifies them. It
// errs toward review: anything it cannot read is not ReadOnly.
func ParseShell(command string) Shell {
	words, ok := splitWords(command)
	if !ok {
		return Shell{Opaque: true, Other: true}
	}
	shell := Shell{ReadOnly: true}
	for _, simple := range words {
		readOnly, call := classify(simple)
		if call != nil {
			shell.Bsk = append(shell.Bsk, *call)
		}
		if !readOnly {
			shell.ReadOnly = false
			if call == nil || !bskActions[call.Action] {
				shell.Other = true
			}
		}
	}
	return shell
}

func classify(words []string) (bool, *BrowserCall) {
	for len(words) > 0 && isAssignment(words[0]) {
		words = words[1:]
	}
	if len(words) == 0 {
		return true, nil
	}
	program := path.Base(words[0])
	if program != "bsk" {
		return readOnlyPrograms[program], nil
	}
	args := words[1:]
	sub := ""
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			sub = a
			break
		}
	}
	call := browserCall(sub, args)
	return bskReadOnly[sub], call
}

func browserCall(action string, args []string) *BrowserCall {
	call := &BrowserCall{Action: action}
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch {
		case a == "--session":
			call.Session = next()
		case strings.HasPrefix(a, "--session="):
			call.Session = strings.TrimPrefix(a, "--session=")
		case a == "--value":
			call.Value = next()
		case strings.HasPrefix(a, "--value="):
			call.Value = strings.TrimPrefix(a, "--value=")
		case a == "--ref":
			call.Ref = next()
		case strings.HasPrefix(a, "--"):
		default:
			positional = append(positional, a)
		}
	}
	// positional[0] is the subcommand.
	if len(positional) > 1 && bskActions[action] {
		if action == "press" {
			call.Value = positional[1]
		} else if call.Ref == "" {
			call.Ref = positional[1]
		}
	}
	return call
}

func isAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if !isNameRune(i, r) {
			return false
		}
	}
	return true
}

// isNameRune reports whether r can appear at position i of a shell variable
// name.
func isNameRune(i int, r rune) bool {
	switch {
	case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		return true
	default:
		return i > 0 && r >= '0' && r <= '9'
	}
}

// splitWords tokenizes like a POSIX shell, enough to find simple commands:
// quotes and escapes join words, control operators end a command, and
// redirections drop their target. It reports false on substitutions and
// unbalanced quotes.
func splitWords(command string) ([][]string, bool) {
	var (
		commands [][]string
		current  []string
		word     strings.Builder
		inWord   bool
		skipNext bool
	)
	endWord := func() {
		if !inWord {
			return
		}
		if skipNext {
			skipNext = false
		} else {
			current = append(current, word.String())
		}
		word.Reset()
		inWord = false
	}
	endCommand := func() {
		endWord()
		if len(current) > 0 {
			commands = append(commands, current)
		}
		current = nil
	}
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\\':
			if i+1 < len(runes) {
				i++
				word.WriteRune(runes[i])
				inWord = true
			}
		case r == '\'':
			end := indexRune(runes, i+1, '\'')
			if end < 0 {
				return nil, false
			}
			word.WriteString(string(runes[i+1 : end]))
			inWord = true
			i = end
		case r == '"':
			j := i + 1
			for ; j < len(runes) && runes[j] != '"'; j++ {
				if runes[j] == '\\' && j+1 < len(runes) {
					j++
					word.WriteRune(runes[j])
					continue
				}
				if runes[j] == '`' || runes[j] == '$' && j+1 < len(runes) && runes[j+1] == '(' {
					return nil, false
				}
				word.WriteRune(runes[j])
			}
			if j >= len(runes) {
				return nil, false
			}
			inWord = true
			i = j
		case r == '`', r == '$' && i+1 < len(runes) && runes[i+1] == '(':
			return nil, false
		case (r == '<' || r == '>') && i+1 < len(runes) && runes[i+1] == '(':
			return nil, false
		case r == '|' || r == ';' || r == '&' || r == '\n':
			if r == '&' && i > 0 && runes[i-1] == '>' {
				// The "&" of ">&2" belongs to the redirection.
				continue
			}
			endCommand()
		case r == '>' || r == '<':
			// A redirection: the descriptor before it and the target after it
			// are not command words.
			if inWord && isDigits(word.String()) {
				word.Reset()
				inWord = false
			} else {
				endWord()
			}
			for i+1 < len(runes) && (runes[i+1] == '>' || runes[i+1] == '&') {
				i++
			}
			if i+1 < len(runes) && runes[i+1] >= '0' && runes[i+1] <= '9' {
				for i+1 < len(runes) && runes[i+1] >= '0' && runes[i+1] <= '9' {
					i++
				}
				continue
			}
			skipNext = true
		case r == ' ' || r == '\t':
			endWord()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	endCommand()
	return commands, true
}

func indexRune(runes []rune, from int, target rune) int {
	for i := from; i < len(runes); i++ {
		if runes[i] == target {
			return i
		}
	}
	return -1
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
