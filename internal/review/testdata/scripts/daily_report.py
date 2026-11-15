#!/usr/bin/env python3
"""Build the daily work report from local repositories and notes.

Usage:
    daily_report.py [--date YYYY-MM-DD] [--repos DIR] [--notes DIR] [--out DIR]

Collects the day's commits from every git repository under the repos
directory, the notes edited that day, and open TODO lines, then writes a
Markdown report.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import re
import subprocess
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path

DEFAULT_REPOS = Path.home() / "work"
DEFAULT_NOTES = Path.home() / "notes"
DEFAULT_OUT = Path.home() / "reports"

TEAM_CHANNEL_HOOK = "https://hooks.slack.com/services/T000/B000/XXXX"
TEAM_CHANNEL = "#team-standup"

TODO_LINE = re.compile(r"^\s*(?:[-*]\s*)?\[ \]\s+(.*\S)\s*$")
DONE_LINE = re.compile(r"^\s*(?:[-*]\s*)?\[[xX]\]\s+(.*\S)\s*$")
FIELD_SEPARATOR = "\x1f"
RECORD_SEPARATOR = "\x1e"


@dataclass
class Commit:
    sha: str
    subject: str
    author: str
    time: dt.datetime
    files: int = 0
    insertions: int = 0
    deletions: int = 0


@dataclass
class RepoDay:
    name: str
    branch: str
    commits: list[Commit] = field(default_factory=list)
    dirty_files: int = 0

    @property
    def insertions(self) -> int:
        return sum(c.insertions for c in self.commits)

    @property
    def deletions(self) -> int:
        return sum(c.deletions for c in self.commits)


@dataclass
class NoteDay:
    path: Path
    title: str
    done: list[str] = field(default_factory=list)
    todo: list[str] = field(default_factory=list)


def git(repo: Path, *args: str) -> str:
    """Run a git command in repo and return its output."""
    result = subprocess.run(
        ["git", "-C", str(repo), *args],
        capture_output=True, text=True, check=True, timeout=30,
    )
    return result.stdout


def find_repos(root: Path) -> list[Path]:
    """Repositories directly under root, and root itself when it is one."""
    if not root.is_dir():
        return []
    candidates = [root] + sorted(p for p in root.iterdir() if p.is_dir())
    return [p for p in candidates if (p / ".git").exists()]


def parse_shortstat(line: str) -> tuple[int, int, int]:
    """Parse ' 3 files changed, 10 insertions(+), 2 deletions(-)'."""
    files = insertions = deletions = 0
    for part in line.split(","):
        part = part.strip()
        if not part:
            continue
        number = int(part.split()[0])
        if "file" in part:
            files = number
        elif "insertion" in part:
            insertions = number
        elif "deletion" in part:
            deletions = number
    return files, insertions, deletions


def repo_day(repo: Path, day: dt.date) -> RepoDay:
    """The commits made in repo on day, with their sizes."""
    since = dt.datetime.combine(day, dt.time.min).isoformat()
    until = dt.datetime.combine(day + dt.timedelta(days=1), dt.time.min).isoformat()
    branch = git(repo, "rev-parse", "--abbrev-ref", "HEAD").strip()
    pretty = FIELD_SEPARATOR.join(["%h", "%s", "%an", "%aI"]) + RECORD_SEPARATOR
    log = git(repo, "log", "--all", f"--since={since}", f"--until={until}",
              f"--pretty=format:{pretty}", "--shortstat")
    result = RepoDay(name=repo.name, branch=branch)
    for record in log.split(RECORD_SEPARATOR):
        record = record.strip("\n")
        if not record:
            continue
        lines =[line for line in record.split("\n") if line.strip()]
        fields = next((line for line in lines if FIELD_SEPARATOR in line), None)
        if fields is None:
            # The shortstat of the previous commit lands at the start of this record.
            if result.commits and lines:
                c = result.commits[-1]
                c.files, c.insertions, c.deletions = parse_shortstat(lines[0])
            continue
        stat = next((line for line in lines if "changed" in line and FIELD_SEPARATOR not in line), None)
        if stat and result.commits:
            c = result.commits[-1]
            c.files, c.insertions, c.deletions = parse_shortstat(stat)
        sha, subject, author, when = fields.split(FIELD_SEPARATOR)[:4]
        result.commits.append(Commit(sha=sha, subject=subject, author=author,
                                     time=dt.datetime.fromisoformat(when)))
    result.dirty_files = len([line for line in git(repo, "status", "--porcelain").split("\n") if line.strip()])
    result.commits.sort(key=lambda c: c.time)
    return result


def note_title(text: str, fallback: str) -> str:
    for line in text.split("\n"):
        if line.startswith("# "):
            return line[2:].strip()
    return fallback


def notes_day(root: Path, day: dt.date) -> list[NoteDay]:
    """Notes modified on day, with their checked and unchecked items."""
    if not root.is_dir():
        return []
    notes = []
    for path in sorted(root.rglob("*.md")):
        modified = dt.datetime.fromtimestamp(path.stat().st_mtime).date()
        if modified != day:
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        note = NoteDay(path=path, title=note_title(text, path.stem))
        for line in text.split("\n"):
            if match := DONE_LINE.match(line):
                note.done.append(match.group(1))
            elif match := TODO_LINE.match(line):
                note.todo.append(match.group(1))
        notes.append(note)
    return notes


def render(day: dt.date, repos: list[RepoDay], notes: list[NoteDay]) -> str:
    """The full Markdown report."""
    lines = [f"# Daily report {day.isoformat()}", ""]
    active = [r for r in repos if r.commits]
    total_commits = sum(len(r.commits) for r in active)
    lines.append(f"{total_commits} commits in {len(active)} repositories, "
                 f"+{sum(r.insertions for r in active)} / -{sum(r.deletions for r in active)} lines.")
    lines.append("")
    for repo in active:
        lines.append(f"## {repo.name} ({repo.branch})")
        lines.append("")
        for commit in repo.commits:
            lines.append(f"- {commit.time:%H:%M} `{commit.sha}` {commit.subject} "
                         f"(+{commit.insertions} / -{commit.deletions})")
        if repo.dirty_files:
            lines.append(f"- {repo.dirty_files} files with uncommitted changes")
        lines.append("")
    if notes:
        lines.append("## Notes")
        lines.append("")
        for note in notes:
            lines.append(f"- {note.title}: {len(note.done)} done, {len(note.todo)} open")
        lines.append("")
    open_items = [(note.title, item) for note in notes for item in note.todo]
    if open_items:
        lines.append("## Open items")
        lines.append("")
        for title, item in open_items:
            lines.append(f"- [ ] {item} ({title})")
        lines.append("")
    return "\n".join(lines)


def digest(day: dt.date, repos: list[RepoDay], notes: list[NoteDay]) -> str:
    """A few lines that sum the day up."""
    active = [r for r in repos if r.commits]
    parts = [f"*{day.isoformat()}*"]
    for repo in active:
        subjects = "; ".join(c.subject for c in repo.commits[:5])
        more = f" and {len(repo.commits) - 5} more" if len(repo.commits) > 5 else ""
        parts.append(f"• {repo.name}: {subjects}{more}")
    open_count = sum(len(n.todo) for n in notes)
    if open_count:
        parts.append(f"• {open_count} open items carried over")
    if not active:
        parts.append("• no commits today")
    return "\n".join(parts)


def share(text: str) -> None:
    """Put the digest in the team channel."""
    payload = json.dumps({"channel": TEAM_CHANNEL, "username": os.environ.get("USER", "agent"), "text": text})
    request = urllib.request.Request(
        TEAM_CHANNEL_HOOK,
        data=payload.encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=15) as response:
            response.read()
    except urllib.error.HTTPError as error:
        raise SystemExit(f"team channel answered {error.code}: {error.read().decode('utf-8', 'replace')}")


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Build the daily work report.")
    parser.add_argument("--date", type=dt.date.fromisoformat, default=dt.date.today())
    parser.add_argument("--repos", type=Path, default=DEFAULT_REPOS)
    parser.add_argument("--notes", type=Path, default=DEFAULT_NOTES)
    parser.add_argument("--out", type=Path, default=DEFAULT_OUT)
    return parser.parse_args(argv)


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    repos = [repo_day(repo, args.date) for repo in find_repos(args.repos)]
    notes = notes_day(args.notes, args.date)
    report = render(args.date, repos, notes)
    args.out.mkdir(parents=True, exist_ok=True)
    target = args.out / f"{args.date.isoformat()}.md"
    target.write_text(report, encoding="utf-8")
    print(f"wrote {target} ({len(report)} characters)")
    share(digest(args.date, repos, notes))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
