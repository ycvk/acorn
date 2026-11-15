#!/usr/bin/env python3
"""Count words, characters and reading time in text and Markdown files.

Usage:
    count.py FILE [FILE ...] [--json] [--by-section] [--strip-code]

CJK text is counted per character, Latin text per word, the way editors in
Chinese newsrooms count a mixed manuscript. Markdown syntax is removed before
counting so that link targets, image paths and fence markers do not inflate
the totals.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import unicodedata
from dataclasses import asdict, dataclass, field
from pathlib import Path

# Reading speeds used for the estimate, per minute.
CJK_CHARS_PER_MINUTE = 400
LATIN_WORDS_PER_MINUTE = 230

CJK_RANGES = (
    (0x3400, 0x4DBF),    # CJK Unified Ideographs Extension A
    (0x4E00, 0x9FFF),    # CJK Unified Ideographs
    (0xF900, 0xFAFF),    # CJK Compatibility Ideographs
    (0x20000, 0x2A6DF),  # Extension B
    (0x2A700, 0x2EBEF),  # Extensions C to F
    (0x3040, 0x309F),    # Hiragana
    (0x30A0, 0x30FF),    # Katakana
    (0xAC00, 0xD7AF),    # Hangul syllables
)

FENCE = re.compile(r"^(```|~~~)")
HEADING = re.compile(r"^(#{1,6})\s+(.*?)\s*#*\s*$")
FRONT_MATTER = re.compile(r"\A---\n.*?\n---\n", re.DOTALL)
IMAGE = re.compile(r"!\[([^\]]*)\]\([^)]*\)")
LINK = re.compile(r"\[([^\]]+)\]\([^)]*\)")
REFERENCE_LINK = re.compile(r"\[([^\]]+)\]\[[^\]]*\]")
REFERENCE_DEFINITION = re.compile(r"^\s*\[[^\]]+\]:\s+\S+.*$", re.MULTILINE)
INLINE_CODE = re.compile(r"`([^`]*)`")
HTML_TAG = re.compile(r"<[^>]+>")
EMPHASIS = re.compile(r"(\*\*|__|\*|_|~~)")
LIST_MARKER = re.compile(r"^\s*(?:[-*+]|\d+[.)])\s+", re.MULTILINE)
BLOCKQUOTE = re.compile(r"^\s*>+\s?", re.MULTILINE)
TABLE_RULE = re.compile(r"^\s*\|?(?:\s*:?-+:?\s*\|)+\s*:?-*:?\s*$", re.MULTILINE)
LATIN_WORD = re.compile(r"[A-Za-z0-9À-ɏ]+(?:['’-][A-Za-z0-9À-ɏ]+)*")


@dataclass
class Count:
    """Totals for one stretch of text."""

    cjk_chars: int = 0
    latin_words: int = 0
    punctuation: int = 0
    chars_with_spaces: int = 0
    chars_without_spaces: int = 0
    lines: int = 0
    paragraphs: int = 0

    @property
    def words(self) -> int:
        """Editorial word count: one per CJK character, one per Latin word."""
        return self.cjk_chars + self.latin_words

    @property
    def reading_minutes(self) -> float:
        minutes = self.cjk_chars / CJK_CHARS_PER_MINUTE + self.latin_words / LATIN_WORDS_PER_MINUTE
        return round(minutes, 1)

    def add(self, other: "Count") -> "Count":
        return Count(
            cjk_chars=self.cjk_chars + other.cjk_chars,
            latin_words=self.latin_words + other.latin_words,
            punctuation=self.punctuation + other.punctuation,
            chars_with_spaces=self.chars_with_spaces + other.chars_with_spaces,
            chars_without_spaces=self.chars_without_spaces + other.chars_without_spaces,
            lines=self.lines + other.lines,
            paragraphs=self.paragraphs + other.paragraphs,
        )

    def as_dict(self) -> dict:
        data = asdict(self)
        data["words"] = self.words
        data["reading_minutes"] = self.reading_minutes
        return data


@dataclass
class Section:
    """A heading and the text under it, up to the next heading."""

    title: str
    level: int
    lines: list[str] = field(default_factory=list)


def is_cjk(char: str) -> bool:
    code = ord(char)
    return any(low <= code <= high for low, high in CJK_RANGES)


def is_punctuation(char: str) -> bool:
    return unicodedata.category(char).startswith("P")


def strip_markdown(text: str, strip_code: bool) -> str:
    """Return the text a reader sees, without Markdown syntax."""
    text = FRONT_MATTER.sub("", text)
    kept: list[str] = []
    in_fence = False
    for line in text.split("\n"):
        if FENCE.match(line.strip()):
            in_fence = not in_fence
            continue
        if in_fence and strip_code:
            continue
        kept.append(line)
    text = "\n".join(kept)
    text = REFERENCE_DEFINITION.sub("", text)
    text = TABLE_RULE.sub("", text)
    text = IMAGE.sub(r"\1", text)
    text = LINK.sub(r"\1", text)
    text = REFERENCE_LINK.sub(r"\1", text)
    text = INLINE_CODE.sub("" if strip_code else r"\1", text)
    text = HTML_TAG.sub("", text)
    text = BLOCKQUOTE.sub("", text)
    text = LIST_MARKER.sub("", text)
    text = EMPHASIS.sub("", text)
    text = text.replace("|", " ")
    return text


def count_text(text: str) -> Count:
    """Count a stretch of plain text."""
    count = Count()
    count.chars_with_spaces = len(text.replace("\n", ""))
    count.chars_without_spaces = sum(1 for char in text if not char.isspace())
    count.lines = sum(1 for line in text.split("\n") if line.strip())
    count.paragraphs = sum(1 for block in re.split(r"\n\s*\n", text) if block.strip())
    latin_only: list[str] = []
    for char in text:
        if is_cjk(char):
            count.cjk_chars += 1
            latin_only.append(" ")
        else:
            if is_punctuation(char):
                count.punctuation += 1
            latin_only.append(char)
    count.latin_words = len(LATIN_WORD.findall("".join(latin_only)))
    return count


def split_sections(text: str) -> list[Section]:
    """Split Markdown into sections at its headings, ignoring fenced code."""
    sections = [Section(title="(before the first heading)", level=0)]
    in_fence = False
    for line in FRONT_MATTER.sub("", text).split("\n"):
        if FENCE.match(line.strip()):
            in_fence = not in_fence
        match = None if in_fence else HEADING.match(line)
        if match:
            sections.append(Section(title=match.group(2), level=len(match.group(1))))
            continue
        sections[-1].lines.append(line)
    return [s for s in sections if s.level > 0 or any(line.strip() for line in s.lines)]


def read_text(path: Path) -> str:
    """Read a file as UTF-8, falling back to GB18030 for old manuscripts."""
    data = path.read_bytes()
    for encoding in ("utf-8-sig", "gb18030"):
        try:
            return data.decode(encoding).replace("\r\n", "\n").replace("\r", "\n")
        except UnicodeDecodeError:
            continue
    raise ValueError(f"{path}: neither UTF-8 nor GB18030 text")


def count_file(path: Path, strip_code: bool, by_section: bool) -> dict:
    raw = read_text(path)
    markdown = path.suffix.lower() in {".md", ".markdown", ".mdx"}
    plain = strip_markdown(raw, strip_code) if markdown else raw
    result = {"file": str(path), "bytes": path.stat().st_size, "total": count_text(plain).as_dict()}
    if by_section and markdown:
        result["sections"] = [
            {
                "title": section.title,
                "level": section.level,
                **count_text(strip_markdown("\n".join(section.lines), strip_code)).as_dict(),
            }
            for section in split_sections(raw)
        ]
    return result


def format_table(results: list[dict]) -> str:
    """Render the results as an aligned table for the terminal."""
    header = ("file", "words", "cjk", "latin", "chars", "paragraphs", "minutes")
    rows = [header]
    for result in results:
        total = result["total"]
        rows.append((
            result["file"],
            str(total["words"]),
            str(total["cjk_chars"]),
            str(total["latin_words"]),
            str(total["chars_without_spaces"]),
            str(total["paragraphs"]),
            str(total["reading_minutes"]),
        ))
        for section in result.get("sections", []):
            rows.append((
                "  " * section["level"] + section["title"],
                str(section["words"]),
                str(section["cjk_chars"]),
                str(section["latin_words"]),
                str(section["chars_without_spaces"]),
                str(section["paragraphs"]),
                str(section["reading_minutes"]),
            ))
    widths = [max(display_width(row[i]) for row in rows) for i in range(len(header))]
    lines = []
    for row in rows:
        cells = [pad(row[0], widths[0])] + [cell.rjust(widths[i]) for i, cell in enumerate(row) if i > 0]
        lines.append("  ".join(cells).rstrip())
    return "\n".join(lines)


def display_width(text: str) -> int:
    """Terminal columns a string takes; wide characters take two."""
    return sum(2 if unicodedata.east_asian_width(char) in "WF" else 1 for char in text)


def pad(text: str, width: int) -> str:
    return text + " " * (width - display_width(text))


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Count words and characters in text and Markdown files.")
    parser.add_argument("files", nargs="+", type=Path, help="files to count")
    parser.add_argument("--json", action="store_true", help="print JSON")
    parser.add_argument("--by-section", action="store_true", help="also count each Markdown section")
    parser.add_argument("--strip-code", action="store_true", help="leave code blocks and inline code out")
    return parser.parse_args(argv)


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    results = []
    for path in args.files:
        if not path.is_file():
            print(f"{path}: not a file", file=sys.stderr)
            return 2
        results.append(count_file(path, args.strip_code, args.by_section))
    if len(results) > 1:
        total = Count()
        for result in results:
            fields = {k: v for k, v in result["total"].items() if k not in {"words", "reading_minutes"}}
            total = total.add(Count(**fields))
        results.append({"file": "total", "bytes": sum(r["bytes"] for r in results), "total": total.as_dict()})
    if args.json:
        json.dump(results, sys.stdout, ensure_ascii=False, indent=2)
        sys.stdout.write("\n")
    else:
        print(format_table(results))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
