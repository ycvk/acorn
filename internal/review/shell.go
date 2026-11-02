package review

import (
	"path"
	"slices"
	"strings"
)

// Shell is a shell command split into the parts review cares about.
type Shell struct {
	// ReadOnly is set when every simple command is a listed program run by
	// its bare name with nothing that makes it run another program, and no
	// redirection can reach the network or write a file bash runs at login,
	// so the command needs no review.
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
	"basename": true, "cat": true, "cd": true, "cut": true, "date": true, "diff": true,
	"dirname": true, "du": true, "echo": true, "egrep": true, "file": true, "grep": true,
	"head": true, "jq": true, "ls": true, "md5sum": true, "printf": true, "pwd": true,
	"readlink": true, "realpath": true, "sha256sum": true, "sleep": true, "sort": true,
	"stat": true, "tail": true, "test": true, "tr": true, "tree": true, "true": true,
	"uniq": true, "wc": true, "[": true,
}

// findActions are the find expressions that run commands or write files.
var findActions = map[string]bool{
	"-delete": true, "-exec": true, "-execdir": true, "-ok": true, "-okdir": true,
	"-fls": true, "-fprint": true, "-fprint0": true, "-fprintf": true,
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
	words, targets, ok := splitWords(command)
	if !ok {
		return Shell{Other: true}
	}
	shell := Shell{ReadOnly: !slices.ContainsFunc(targets, riskyTarget)}
	shell.Other = !shell.ReadOnly
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

// loginFiles are the files a login shell runs, which the workstation's
// commands are. Whatever is written there runs, unreviewed, with the next
// command.
var loginFiles = map[string]bool{
	".bash_aliases": true, ".bash_login": true, ".bash_logout": true,
	".bash_profile": true, ".bashrc": true, ".profile": true,
}

// riskyTarget reports whether a redirection target may be one of bash's
// network paths, /dev/tcp/host/port and /dev/udp/host/port, which open a
// connection, or a file a login shell runs; a target built from a variable
// or a pattern may be one too.
func riskyTarget(target string) bool {
	clean := path.Clean(target)
	return strings.HasPrefix(clean, "/dev/tcp/") || strings.HasPrefix(clean, "/dev/udp/") ||
		strings.ContainsAny(target, "$*?[") || loginFiles[path.Base(clean)]
}

func classify(words []string) (bool, *BrowserCall) {
	for len(words) > 0 && isAssignment(words[0]) {
		if !plainAssignment(words[0]) {
			return false, nil
		}
		words = words[1:]
	}
	if len(words) == 0 {
		return true, nil
	}
	switch program := words[0]; program {
	case "find":
		return !slices.ContainsFunc(words[1:], func(w string) bool { return findActions[w] }), nil
	case "sort":
		return !slices.ContainsFunc(words[1:], runsProgram), nil
	case "printf":
		// printf -v assigns a variable, PATH among them.
		return len(words) < 2 || !strings.HasPrefix(words[1], "-v"), nil
	case "bsk":
		return classifyBsk(words[1:])
	default:
		return readOnlyPrograms[program], nil
	}
}

func classifyBsk(args []string) (bool, *BrowserCall) {
	sub := ""
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			sub = a
			break
		}
	}
	return bskReadOnly[sub], browserCall(sub, args)
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

// plainAssignment reports whether an assignment sets a variable only the
// command's own words use: a lowercase name, or one of bsk's settings. Names
// such as PATH, IFS, BASH_ENV and LD_PRELOAD change which program runs.
func plainAssignment(word string) bool {
	name, _, _ := strings.Cut(word, "=")
	return strings.HasPrefix(name, "BSK_") || name == strings.ToLower(name)
}

// runsProgram reports whether a sort argument is --compress-program, which
// runs the program it names, or an abbreviation of it that getopt accepts.
func runsProgram(arg string) bool {
	name, ok := strings.CutPrefix(arg, "--")
	if !ok {
		return false
	}
	name, _, _ = strings.Cut(name, "=")
	return name != "" && strings.HasPrefix("compress-program", name) && !strings.HasPrefix("check", name)
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
// redirection targets are returned apart from the command words; the
// delimiter of a here-document and the word of a here-string are neither.
// It reports false on substitutions and unbalanced quotes.
func splitWords(command string) ([][]string, []string, bool) {
	var (
		commands   [][]string
		targets    []string
		current    []string
		word       strings.Builder
		inWord     bool
		skipNext   bool
		targetNext bool
	)
	endWord := func() {
		if !inWord {
			return
		}
		switch {
		case skipNext && targetNext:
			targets = append(targets, word.String())
			skipNext, targetNext = false, false
		case skipNext:
			skipNext = false
		default:
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
				return nil, nil, false
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
					return nil, nil, false
				}
				word.WriteRune(runes[j])
			}
			if j >= len(runes) {
				return nil, nil, false
			}
			inWord = true
			i = j
		case r == '`', r == '$' && i+1 < len(runes) && runes[i+1] == '(':
			return nil, nil, false
		case (r == '<' || r == '>') && i+1 < len(runes) && runes[i+1] == '(':
			return nil, nil, false
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
			heredoc := r == '<' && i+1 < len(runes) && runes[i+1] == '<'
			for i+1 < len(runes) && (runes[i+1] == '>' || runes[i+1] == '<' || runes[i+1] == '&' || runes[i+1] == '|') {
				i++
			}
			if i+1 < len(runes) && runes[i+1] >= '0' && runes[i+1] <= '9' {
				for i+1 < len(runes) && runes[i+1] >= '0' && runes[i+1] <= '9' {
					i++
				}
				continue
			}
			skipNext, targetNext = true, !heredoc
		case r == ' ' || r == '\t':
			endWord()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	endCommand()
	return commands, targets, true
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
