package review

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// Shell is a shell command split into the parts review cares about.
type Shell struct {
	// ReadOnly is set when every simple command is a listed program run by
	// its bare name with nothing that makes it run another program or write
	// a file of its own, and no redirection can reach the network or write
	// a file bash runs at login, so the command needs no review.
	ReadOnly bool
	// Other is set when a part that is neither read-only nor a bsk page
	// action needs review as a whole command.
	Other bool
	// Bsk holds every bsk command, in order.
	Bsk []BrowserCall
	// Shows is the session whose page the command's output is: set when the
	// command only has bsk go to, reload or observe pages of that one
	// session, after page actions on it at most, and nothing else writes
	// output but a cut of its first lines.
	Shows string
	// Moves is set when the command may give a session new refs without
	// Shows: a page shown next to other output or into a file, a bsk command
	// ParseShell does not read, or a program that could run bsk itself.
	Moves bool
}

// Page reads the output of a command that Shows a session. whole is false
// when the output lacks a report its parts owe, an address for each time bsk
// went somewhere and the start of an observation for each observe: a part
// left no trace, so the session may be past what the output shows.
func (s Shell) Page(output string) (page Page, whole bool) {
	went, observed := 0, 0
	for _, call := range s.Bsk {
		switch call.Action {
		case "observe", "snapshot":
			observed++
		case "navigate", "navigate-back", "navigate-forward", "reload":
			went++
		}
	}
	page, addresses, observations := readPage(output)
	return page, addresses == went && observations == observed
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
// for a page action, the element and the value. The element is a ref,
// written @eN, or a CSS selector. Unsure is set when review cannot tell what
// the action acts on: its arguments are not ones ParseShell reads as bsk
// does, or the command may give the session new refs before the action runs.
type BrowserCall struct {
	Action   string
	Ref      string
	Selector string
	Value    string
	Session  string
	Unsure   bool
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

// bskShows are the bsk subcommands that print the page their session is on:
// its address after going somewhere, or what its refs name.
var bskShows = map[string]bool{
	"navigate": true, "navigate-back": true, "navigate-forward": true,
	"observe": true, "reload": true, "snapshot": true,
}

// bskStill are the bsk subcommands that leave every session's refs as they
// are and print text of their own. session starts, stops and lists sessions.
var bskStill = map[string]bool{
	"browsers": true, "console": true, "doctor": true, "get-html": true, "help": true,
	"logs": true, "network": true, "screenshot": true, "session": true, "status": true,
	"wait-ms": true,
}

// bskOptions are the options ParseShell reads on a bsk command, true for one
// that takes a value. bsk's other options pick another tab, another way to
// click or type, or a part of the page, so a command carrying one is not
// read.
var bskOptions = map[string]bool{
	"--session": true, "--ref": true, "--selector": true, "--value": true, "--cursor": true,
	"--max-depth": true, "--max-tokens": true, "--timeout": true, "--wait-until": true,
	"--hard": false, "--json": false, "--quiet": false, "--verbose": false,
}

// bskShowOptions are the options a command may carry and still print the
// page in the lines readPage reads.
var bskShowOptions = map[string]bool{
	"--session": true, "--cursor": true, "--hard": true, "--max-depth": true,
	"--max-tokens": true, "--timeout": true, "--wait-until": true,
}

// pageEffect is what a simple command does to the refs of the browser
// sessions and to the command's output, as far as its words tell.
type pageEffect int

const (
	// pageMoves may give a session new refs, or runs something that could.
	pageMoves pageEffect = iota
	// pageStill leaves the refs as they are and prints text of its own.
	pageStill
	// pageQuiet leaves the refs as they are and prints no text a page or
	// the command's words shape: a cut of the lines it is piped, or a pause.
	pageQuiet
	// pageActs is a page action ParseShell reads in full: the refs stay and
	// bsk reports it in a line that holds no page text.
	pageActs
	// pageShows prints the page of the session it names: the address it
	// reached, or an observation.
	pageShows
)

// ParseShell splits a command into simple commands and classifies them. It
// errs toward review: anything it cannot read is not ReadOnly.
func ParseShell(command string) Shell {
	script, ok := splitWords(command)
	if !ok {
		return Shell{Other: true, Moves: true}
	}
	shell := Shell{ReadOnly: !slices.ContainsFunc(script.targets, riskyTarget)}
	shell.Other = !shell.ReadOnly
	calls := make([]*BrowserCall, len(script.commands))
	effects := make([]pageEffect, len(script.commands))
	for i, simple := range script.commands {
		var readOnly bool
		readOnly, calls[i], effects[i] = classify(simple)
		if !readOnly {
			shell.ReadOnly = false
			if calls[i] == nil || !bskActions[calls[i].Action] {
				shell.Other = true
			}
		}
	}
	// A page action is judged by what its ref named when the command was
	// reviewed, so nothing may give the session new refs before it runs.
	renews := func(e pageEffect) bool { return e == pageMoves || e == pageShows }
	for i, call := range calls {
		if call == nil || !bskActions[call.Action] || call.Unsure {
			continue
		}
		if slices.ContainsFunc(effects[:i], renews) || script.overlaps(i) && slices.ContainsFunc(effects[i+1:], renews) {
			call.Unsure, effects[i] = true, pageMoves
		}
	}
	for _, call := range calls {
		if call != nil {
			shell.Bsk = append(shell.Bsk, *call)
		}
	}
	shell.Shows, shell.Moves = script.shown(calls, effects)
	return shell
}

// shown tells what a command's parts leave of the page memory: the session
// whose page the output is, or whether a session may have new refs that the
// output does not show in full.
func (s script) shown(calls []*BrowserCall, effects []pageEffect) (shows string, moves bool) {
	if slices.Contains(effects, pageMoves) {
		return "", true
	}
	if !slices.Contains(effects, pageShows) {
		return "", false
	}
	// The output is the page when bsk wrote all of it, about one session,
	// one command after the other.
	whole := s.heres == 0 && !slices.ContainsFunc(s.targets, func(t string) bool { return t != "/dev/null" })
	sessions := map[string]bool{}
	for i, effect := range effects {
		switch effect {
		case pageStill:
			whole = false
		case pageActs, pageShows:
			sessions[calls[i].Session] = true
		}
		switch s.joins[i] {
		case joinBack:
			whole = false
		case joinPipe:
			whole = whole && i+1 < len(effects) && effects[i+1] == pageQuiet
		}
	}
	if !whole || len(sessions) != 1 {
		return "", true
	}
	for session := range sessions {
		shows = session
	}
	return shows, false
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
// connection, or a file a login shell runs; a target built from a variable,
// a brace list or a pattern may be one too.
func riskyTarget(target string) bool {
	clean := path.Clean(target)
	return strings.HasPrefix(clean, "/dev/tcp/") || strings.HasPrefix(clean, "/dev/udp/") ||
		strings.ContainsAny(target, "$*?[{") || LoginFile(target)
}

// LoginFile reports whether name is a file a login shell runs, in whatever
// directory.
func LoginFile(name string) bool {
	return loginFiles[path.Base(path.Clean(name))]
}

// classify judges a simple command: whether it is read-only, the bsk command
// it is, and what it does to the pages. A program judged by its arguments is
// read-only only when every argument is the word the program gets: an open
// one can become the option the check looks for.
func classify(words []word) (bool, *BrowserCall, pageEffect) {
	assigned := false
	for len(words) > 0 && isAssignment(words[0].text) {
		if !plainAssignment(words[0].text) {
			return false, nil, pageMoves
		}
		words, assigned = words[1:], true
	}
	if len(words) == 0 {
		return true, nil, pageStill
	}
	program := words[0].text
	if program == "bsk" {
		return classifyBsk(words[1:], assigned)
	}
	args, literal := make([]string, 0, len(words)-1), true
	for _, w := range words[1:] {
		args = append(args, w.text)
		literal = literal && !w.open
	}
	var readOnly bool
	switch program {
	case "find":
		readOnly = literal && !slices.ContainsFunc(args, func(a string) bool { return findActions[a] })
	case "sort":
		readOnly = literal && !slices.ContainsFunc(args, sortRunsOrWrites)
	case "uniq":
		// uniq writes to its second operand.
		readOnly = literal && operands(args) < 2
	case "tree":
		readOnly = literal && !slices.ContainsFunc(args, func(a string) bool { return shortOption(a, 'o') })
	case "printf":
		// printf -v assigns a variable, PATH among them; only its first
		// argument can be the option.
		readOnly = len(args) == 0 || !words[1].open && !strings.HasPrefix(args[0], "-v")
	default:
		readOnly = readOnlyPrograms[program]
	}
	switch {
	case !readOnly:
		return false, nil, pageMoves
	case literal && !assigned && quiet(program, args):
		return true, nil, pageQuiet
	default:
		return true, nil, pageStill
	}
}

// sleepTime is a pause as sleep takes it.
var sleepTime = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?[smhd]?$`)

// quiet reports whether a program run this way prints nothing of its own:
// sleep for a time it can read, or head cutting what it is piped to a count
// of lines.
func quiet(program string, args []string) bool {
	switch {
	case program == "sleep":
		return !slices.ContainsFunc(args, func(a string) bool { return !sleepTime.MatchString(a) })
	case program != "head":
		return false
	case len(args) == 1: // -200, -n200
		count, ok := strings.CutPrefix(args[0], "-")
		return ok && isDigits(strings.TrimPrefix(count, "n"))
	case len(args) == 2: // -n 200, -n -5
		return args[0] == "-n" && isDigits(strings.TrimPrefix(args[1], "-"))
	default:
		return len(args) == 0
	}
}

// classifyBsk judges a bsk command. Its subcommand says whether it is
// read-only. What it does to the pages counts as known only when ParseShell
// reads every argument as bsk does; a command it does not read may have
// given a session new refs.
func classifyBsk(args []word, assigned bool) (bool, *BrowserCall, pageEffect) {
	cmd := readBsk(args)
	call := browserCall(cmd)
	// The output is bsk's own report, line for line: no setting changes its
	// form and no word of the command can break a line of it.
	plain := cmd.plain && !assigned
	effect := pageMoves
	switch {
	case cmd.sub == "wait-ms" && cmd.read && plain:
		effect = pageQuiet
	case bskStill[cmd.sub]:
		effect = pageStill
	case call.Unsure:
	case bskActions[cmd.sub] && plain && cmd.sub != "select":
		effect = pageActs
	case bskActions[cmd.sub]:
		// select reports the labels of the options it picked, which are the
		// page's text.
		effect = pageStill
	case bskShows[cmd.sub] && plain && call.Session != "" && len(cmd.positional) <= 1 && cmd.only(bskShowOptions):
		effect = pageShows
	}
	return bskReadOnly[cmd.sub], call, effect
}

// bskCommand is a bsk command line taken apart as bsk does.
type bskCommand struct {
	sub        string
	positional []string
	options    map[string][]string
	// read is false when ParseShell cannot tell what bsk gets: a word bash
	// expands further, an option not in bskOptions, or an option without
	// its value.
	read bool
	// plain is false when a word holds a line break, which bsk may print
	// back.
	plain bool
}

// only reports whether every option of the command is in allowed.
func (c bskCommand) only(allowed map[string]bool) bool {
	for name := range c.options {
		if !allowed[name] {
			return false
		}
	}
	return true
}

// readBsk takes a bsk command's arguments apart. bsk takes its subcommand
// first, after flags at most, so an option that takes a value or is not
// known before it leaves the subcommand unknown.
func readBsk(args []word) bskCommand {
	cmd := bskCommand{options: map[string][]string{}, read: true, plain: true}
	text := func(w word) string {
		cmd.read = cmd.read && !w.open
		cmd.plain = cmd.plain && !strings.ContainsAny(w.text, "\n\r")
		return w.text
	}
	named, early := false, false
	for i := 0; i < len(args); i++ {
		a := text(args[i])
		if a == "-" || !strings.HasPrefix(a, "-") {
			if named {
				cmd.positional = append(cmd.positional, a)
			} else {
				named, cmd.sub = true, a
			}
			continue
		}
		name, value, inline := strings.Cut(a, "=")
		if !inline && strings.Trim(name, "v") == "-" {
			name = "--verbose" // -v, -vv
		}
		takes, known := bskOptions[name]
		if !known || inline && !takes {
			cmd.read, early = false, early || !named
			continue
		}
		early = early || takes && !named
		if takes && !inline {
			if i+1 == len(args) {
				cmd.read = false
				continue
			}
			i++
			value = text(args[i])
		}
		cmd.options[name] = append(cmd.options[name], value)
	}
	if early {
		cmd.sub, cmd.read = "", false
	}
	return cmd
}

// browserCall is the bsk command as review judges it. A page action names its
// element once: as its argument, with --ref or with --selector.
func browserCall(cmd bskCommand) *BrowserCall {
	call := &BrowserCall{Action: cmd.sub, Unsure: !cmd.read}
	once := func(name string) (string, bool) {
		values := cmd.options[name]
		if len(values) == 0 {
			return "", false
		}
		call.Unsure = call.Unsure || len(values) > 1
		return values[0], true
	}
	call.Session, _ = once("--session")
	if !bskActions[cmd.sub] {
		return call
	}
	elements := cmd.positional
	if cmd.sub == "press" {
		// press takes the key as its argument and the element as an option.
		call.Unsure = call.Unsure || len(elements) != 1
		call.Value, elements = strings.Join(elements, " "), nil
	} else {
		// select takes --value once for each option it picks.
		call.Unsure = call.Unsure || cmd.sub == "fill" && len(cmd.options["--value"]) > 1
		call.Value = strings.Join(cmd.options["--value"], ", ")
	}
	ref, byRef := once("--ref")
	selector, bySelector := once("--selector")
	switch {
	case len(elements) == 1 && !byRef && !bySelector:
		if call.Ref = refName(elements[0]); call.Ref == "" {
			// bsk may take as a ref what only looks like one here.
			call.Selector, call.Unsure = elements[0], call.Unsure || refLike(elements[0])
		}
	case len(elements) == 0 && byRef && !bySelector:
		call.Ref = refName(ref)
		call.Unsure = call.Unsure || call.Ref == ""
	case len(elements) == 0 && bySelector && !byRef:
		call.Selector = selector
	case len(elements) > 0 || byRef:
		call.Unsure = true
	}
	return call
}

// refName is a snapshot ref as page observations write it, @eN, from either
// form bsk takes, @eN or eN; it is empty for other text.
func refName(text string) string {
	digits, ok := strings.CutPrefix(strings.TrimPrefix(text, "@"), "e")
	if !ok || !isDigits(digits) {
		return ""
	}
	return "@e" + digits
}

// refLike reports whether text could be a ref written another way: it
// starts with @, or is an e, in either case, followed by a number however
// it is signed or spaced.
func refLike(text string) bool {
	number, ok := strings.CutPrefix(strings.ToLower(text), "e")
	return strings.HasPrefix(text, "@") || ok && number != "" && strings.Trim(number, "+-_ 0123456789") == ""
}

// plainAssignment reports whether an assignment sets a variable only the
// command's own words use: a lowercase name, or one of bsk's settings. Names
// such as PATH, IFS, BASH_ENV and LD_PRELOAD change which program runs.
func plainAssignment(word string) bool {
	name, _, _ := strings.Cut(word, "=")
	return strings.HasPrefix(name, "BSK_") || name == strings.ToLower(name)
}

// sortRunsOrWrites reports whether a sort argument is --compress-program,
// which runs the program it names, or --output or -o, which write the file
// they name; a long option counts in any abbreviation getopt accepts.
func sortRunsOrWrites(arg string) bool {
	name, ok := strings.CutPrefix(arg, "--")
	if !ok {
		return shortOption(arg, 'o')
	}
	name, _, _ = strings.Cut(name, "=")
	return name != "" && (strings.HasPrefix("output", name) || strings.HasPrefix("compress-program", name) && !strings.HasPrefix("check", name))
}

// shortOption reports whether arg is a group of short options that may hold
// flag; a value written onto an option counts as more of the group.
func shortOption(arg string, flag byte) bool {
	return strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.IndexByte(arg, flag) > 0
}

// operands counts the arguments that are not options. An option's value
// written apart from it counts as one.
func operands(args []string) int {
	n := 0
	for i, a := range args {
		if a == "--" {
			return n + len(args) - i - 1
		}
		if a == "-" || !strings.HasPrefix(a, "-") {
			n++
		}
	}
	return n
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

// word is a command word with its quotes and escapes resolved. open is set
// when bash expands it further than splitWords does: a parameter, a brace
// list or a pattern in it can make it other words than the text shows.
type word struct {
	text string
	open bool
}

// join is how a simple command is tied to the one after it.
type join int

const (
	// joinThen is ; or a newline: the next command waits for this one's list.
	joinThen join = iota
	// joinIf is && or ||: the next waits for this one, in the same list.
	joinIf
	// joinPipe is |: the next reads this one's output and runs beside it.
	joinPipe
	// joinBack is &: this one's list runs in the background.
	joinBack
)

// script is a command as splitWords reads it: its simple commands, how each
// is tied to the next, the files its redirections name, and how many
// here-documents and here-strings feed its commands. Their text is not kept.
type script struct {
	commands [][]word
	joins    []join
	targets  []string
	heres    int
}

// overlaps reports whether command i may still be running when a later one
// starts: it feeds a pipe, or its list runs in the background.
func (s script) overlaps(i int) bool {
	if s.joins[i] == joinPipe {
		return true
	}
	for _, tie := range s.joins[i:] {
		switch tie {
		case joinBack:
			return true
		case joinThen:
			return false
		}
	}
	return false
}

// splitWords tokenizes like a POSIX shell, enough to find simple commands:
// quotes and escapes join words, control operators end a command, and
// redirection targets are returned apart from the command words; the
// delimiter of a here-document and the word of a here-string are neither.
// It reports false on substitutions, unbalanced quotes and parentheses,
// which group commands or define a function under a program's name.
func splitWords(command string) (script, bool) {
	var (
		commands   [][]word
		joins      []join
		targets    []string
		heres      int
		current    []word
		text       strings.Builder
		open       bool
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
			targets = append(targets, text.String())
			skipNext, targetNext = false, false
		case skipNext:
			skipNext = false
			heres++
		default:
			current = append(current, word{text: text.String(), open: open})
		}
		text.Reset()
		inWord, open = false, false
	}
	endCommand := func(tie join) {
		endWord()
		if len(current) > 0 {
			commands = append(commands, current)
			joins = append(joins, tie)
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
				// A backslash before a newline continues the line.
				if runes[i] != '\n' {
					text.WriteRune(runes[i])
					inWord = true
				}
			}
		case r == '\'':
			end := indexRune(runes, i+1, '\'')
			if end < 0 {
				return script{}, false
			}
			text.WriteString(string(runes[i+1 : end]))
			inWord = true
			i = end
		case r == '"':
			j := i + 1
			for ; j < len(runes) && runes[j] != '"'; j++ {
				if runes[j] == '\\' && j+1 < len(runes) {
					j++
					if runes[j] != '\n' {
						text.WriteRune(runes[j])
					}
					continue
				}
				if runes[j] == '`' || runes[j] == '$' && j+1 < len(runes) && runes[j+1] == '(' {
					return script{}, false
				}
				open = open || runes[j] == '$'
				text.WriteRune(runes[j])
			}
			if j >= len(runes) {
				return script{}, false
			}
			inWord = true
			i = j
		case r == '`', r == '$' && i+1 < len(runes) && runes[i+1] == '(':
			return script{}, false
		case r == '(' || r == ')':
			return script{}, false
		case r == '|' || r == ';' || r == '&' || r == '\n':
			if r == '&' && i > 0 && runes[i-1] == '>' {
				// The "&" of ">&2" belongs to the redirection.
				continue
			}
			next := rune(0)
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			tie := joinThen
			switch {
			case r == '|' && next == '|', r == '&' && next == '&':
				tie = joinIf
				i++
			case r == '|':
				tie = joinPipe
				if next == '&' {
					// "|&" pipes the errors too.
					i++
				}
			case r == '&':
				tie = joinBack
			}
			endCommand(tie)
		case r == '>' || r == '<':
			// A redirection: the descriptor before it and the target after it
			// are not command words.
			if inWord && isDigits(text.String()) {
				text.Reset()
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
			open = open || strings.ContainsRune("$*?[{", r)
			text.WriteRune(r)
			inWord = true
		}
	}
	endCommand(joinThen)
	return script{commands: commands, joins: joins, targets: targets, heres: heres}, true
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
