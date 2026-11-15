package review

import (
	"path"
	"slices"
	"strings"
)

// ranFile is a file a simple command runs, as its words name it. placed is
// false when a word does not say which file bash hands on.
type ranFile struct {
	name    string
	placed  bool
	program bool
}

// systemDirs are where the image keeps its programs: a path into one names
// the program its last part would run as a bare name.
var systemDirs = map[string]bool{
	"/bin": true, "/sbin": true, "/usr/bin": true, "/usr/sbin": true,
	"/usr/local/bin": true, "/usr/local/sbin": true,
}

// ran are the files a simple command runs as programs: its program when
// that is named by a path outside the system's directories, and the file a
// shell, python or node is given, or source. Other programs that run a file,
// a module or a task by its name are not read.
func ran(words []word) []ranFile {
	words, _ = lead(words)
	if len(words) == 0 {
		return nil
	}
	name, placed := literal(words[0])
	var runs []ranFile
	switch {
	case !placed && strings.Contains(words[0].text, "/"):
		// A program under a directory only bash can tell.
		return []ranFile{{program: true}}
	case !placed:
		return nil
	case strings.Contains(name, "/"):
		if !systemDirs[path.Dir(path.Clean(name))] {
			runs = append(runs, ranFile{name: name, placed: true, program: true})
		}
		name = path.Base(name)
	}
	args := words[1:]
	if name == "source" || name == "." {
		if len(args) == 0 {
			return runs
		}
		file, ok := literal(args[0])
		return append(runs, ranFile{name: file, placed: ok})
	}
	r, ok := runnerOf(name)
	if !ok {
		return runs
	}
	if file, found, placed := r.file(args); found {
		runs = append(runs, ranFile{name: file, placed: placed})
	}
	return runs
}

// keywords start a command inside a compound one; the command after one is
// read as any other.
var keywords = map[string]bool{
	"!": true, "{": true, "do": true, "elif": true, "else": true, "if": true,
	"then": true, "time": true, "until": true, "while": true,
}

// starters are programs that run the words after them as a command. They are
// read that way when the next word is not an option of theirs; timeout takes
// a time first.
var starters = map[string]bool{
	"command": true, "env": true, "exec": true, "nice": true, "nohup": true,
	"setsid": true, "sudo": true, "timeout": true,
}

// lead takes off what a simple command starts with before the program it
// runs: assignments, shell keywords and programs that only start another.
// led is set when a keyword or such a program was taken off.
func lead(words []word) (rest []word, led bool) {
	for len(words) > 0 {
		name := words[0].text
		switch {
		case isAssignment(name):
		case keywords[name]:
			led = true
		case starters[name] && len(words) > 1 && !strings.HasPrefix(words[1].text, "-") && (name != "timeout" || sleepTime.MatchString(words[1].text)):
			led = true
			if name == "timeout" {
				words = words[1:]
			}
		default:
			return words, led
		}
		words = words[1:]
	}
	return nil, led
}

// literal is the text bash hands a program for w, where the word says: the
// word itself when bash expands it no further, or with ~ for a leading $HOME
// when nothing else in it expands.
func literal(w word) (string, bool) {
	if !w.open {
		return w.text, true
	}
	for _, home := range []string{"$HOME/", "${HOME}/"} {
		if rest, ok := strings.CutPrefix(w.text, home); ok && !strings.ContainsAny(rest, "$*?[{") {
			return "~/" + rest, true
		}
	}
	return "", false
}

// runner is how a program that runs a file takes its options, so the file
// can be told from them.
type runner struct {
	// flags are its one-letter options that take no value.
	flags string
	// inline are the one-letter options that make it run text of the
	// command, a module or its input in place of a file, or only print.
	inline string
	// long are its long options that take no value, and inlineLong those
	// that make it run no file. With assigned set, a long option that
	// carries its value after = is read too.
	long       []string
	inlineLong []string
	assigned   bool
}

var (
	shellRunner = runner{
		flags: "abefhiklmnprtuvxBCEHPT", inline: "cs",
		long:       []string{"--login", "--noprofile", "--norc", "--posix", "--restricted", "--verbose"},
		inlineLong: []string{"--help", "--version"},
	}
	pythonRunner = runner{
		flags: "bBdEiIOPqRsSuvx", inline: "cmhV?",
		inlineLong: []string{"--help", "--version"},
	}
	nodeRunner = runner{
		flags: "ci", inline: "ephv", assigned: true,
		long:       []string{"--enable-source-maps", "--no-deprecation", "--no-warnings", "--trace-warnings"},
		inlineLong: []string{"--eval", "--help", "--print", "--version"},
	}
	runners = map[string]runner{
		"bash": shellRunner, "dash": shellRunner, "ksh": shellRunner, "sh": shellRunner, "zsh": shellRunner,
		"python": pythonRunner, "node": nodeRunner, "nodejs": nodeRunner,
	}
)

// runnerOf is the runner a program's name stands for; python may carry its
// version.
func runnerOf(name string) (runner, bool) {
	if version, ok := strings.CutPrefix(name, "python"); ok && strings.Trim(version, "0123456789.") == "" {
		name = "python"
	}
	r, ok := runners[name]
	return r, ok
}

// file is the file r runs when given args. found is false when it runs
// none: text of the command, a module, its input, or nothing. placed is
// false when an option that is not listed, or a word bash expands further,
// leaves open which file it is.
func (r runner) file(args []word) (name string, found, placed bool) {
	for i, w := range args {
		a := w.text
		switch {
		case a == "-":
			return "", false, true
		case a == "--":
			if i+1 == len(args) {
				return "", false, true
			}
			name, placed = literal(args[i+1])
			return name, true, placed
		case !strings.HasPrefix(a, "-"):
			name, placed = literal(w)
			return name, true, placed
		case w.open:
			return "", true, false
		case strings.HasPrefix(a, "--"):
			option, _, assigned := strings.Cut(a, "=")
			switch {
			case slices.Contains(r.inlineLong, option):
				return "", false, true
			case assigned && r.assigned, !assigned && slices.Contains(r.long, option):
			default:
				return "", true, false
			}
		default:
			for _, flag := range a[1:] {
				if strings.ContainsRune(r.inline, flag) {
					return "", false, true
				}
				if !strings.ContainsRune(r.flags, flag) {
					return "", true, false
				}
			}
		}
	}
	return "", false, true
}

// place is the directory a part of a command starts in, as far as the
// command's own cd tells: relative to where the command starts, absolute or
// under ~. unknown is set after a change of directory ParseShell does not
// follow.
type place struct {
	dir     string
	unknown bool
}

// in is name as a path from where the command starts.
func (p place) in(name string) (string, bool) {
	switch {
	case rooted(name):
		return name, true
	case p.unknown:
		return "", false
	default:
		return path.Join(p.dir, name), true
	}
}

// run is the file f names for a part that starts here. One the command
// writes with a redirection has no text yet.
func (p place) run(f ranFile, targets []string) Run {
	name, ok := p.in(f.name)
	written := slices.ContainsFunc(targets, func(t string) bool { return path.Clean(t) == path.Clean(f.name) })
	if !f.placed || !ok || written {
		return Run{Program: f.program}
	}
	return Run{Path: name, Program: f.program}
}

// after is the place the next part starts in once words ran. A cd is
// followed when it has one argument bash expands no further and runs in the
// shell that runs the next part; aside says it does not, being part of a
// pipeline or of a list in the background. pushd, popd and any other cd
// leave the place unknown.
func (p place) after(words []word, aside bool) place {
	words, led := lead(words)
	if len(words) == 0 {
		return p
	}
	switch words[0].text {
	case "pushd", "popd":
		return place{unknown: true}
	case "cd":
	default:
		return p
	}
	args := words[1:]
	switch {
	case led || aside || len(args) > 1:
		return place{unknown: true}
	case len(args) == 0:
		return place{dir: "~"}
	}
	dir, ok := literal(args[0])
	if !ok || strings.HasPrefix(dir, "-") {
		return place{unknown: true}
	}
	to, ok := p.in(dir)
	return place{dir: to, unknown: !ok}
}
