#!/usr/bin/env python3
"""Validate the mkdocs site under ``docs/docs``.

``mkdocs build --strict`` reports a missing page referenced from the nav, but it
does not report a page that no nav entry reaches, a dead in-page anchor, or a
relative link to a file that does not exist. Those three classes of drift have
recurred in this repository, so they are checked here.

Usage:
    python scripts/check_docs.py [--content-dir docs/docs/content]
                                 [--mkdocs-yml docs/docs/mkdocs.yml]

Exits 0 when the documentation is consistent, 1 otherwise.
"""

from __future__ import annotations

import argparse
import pathlib
import re
import sys
import unicodedata

HEADING_RE = re.compile(r"^(#{1,6})\s+(.+?)\s*#*\s*$")
ANCHOR_RE = re.compile(r"\]\(#([^)\s]+)\)")
LINK_RE = re.compile(r"\]\(([^)\s]+)\)")
FENCE_RE = re.compile(r"^\s*```")
NAV_ENTRY_RE = re.compile(r':\s*"?([\w\-./]+\.md)"?')
LINKABLE_SUFFIXES = (
    ".md",
    ".png",
    ".jpg",
    ".jpeg",
    ".gif",
    ".svg",
    ".css",
    ".js",
    ".yaml",
    ".yml",
    ".toml",
    ".json",
)


def slugify(value: str) -> str:
    """Mirror the slug python-markdown's toc extension generates for a heading."""
    value = unicodedata.normalize("NFKD", value).encode("ascii", "ignore").decode("ascii")
    value = re.sub(r"[^\w\s-]", "", value).strip().lower()
    return re.sub(r"[-\s]+", "-", value)


def iter_lines(path: pathlib.Path):
    """Yield (line_number, text, in_fence) for every line of a markdown file."""
    in_fence = False
    for number, text in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        if FENCE_RE.match(text):
            in_fence = not in_fence
            yield number, text, True
            continue
        yield number, text, in_fence


def page_slugs(path: pathlib.Path) -> set[str]:
    slugs = set()
    for _, text, in_fence in iter_lines(path):
        if in_fence:
            continue
        match = HEADING_RE.match(text)
        if match:
            slug = slugify(match.group(2))
            if slug:
                slugs.add(slug)
    return slugs


def is_moved_stub(path: pathlib.Path) -> bool:
    first = path.read_text(encoding="utf-8").splitlines()[:1]
    return bool(first) and first[0].strip().startswith("# Moved")


def check(content_dir: pathlib.Path, mkdocs_yml: pathlib.Path) -> list[str]:
    problems: list[str] = []
    nav_text = mkdocs_yml.read_text(encoding="utf-8")
    nav_entries = sorted(set(NAV_ENTRY_RE.findall(nav_text)))
    pages = sorted(content_dir.rglob("*.md"))

    for entry in nav_entries:
        if not (content_dir / entry).is_file():
            problems.append(f"nav entry points at a missing file: {entry}")

    for page in pages:
        relative = page.relative_to(content_dir).as_posix()
        if relative not in nav_entries and not is_moved_stub(page):
            problems.append(f"page is not reachable from the nav: {relative}")

    for page in pages:
        relative = page.relative_to(content_dir).as_posix()
        slugs = page_slugs(page)
        for number, text, in_fence in iter_lines(page):
            if in_fence:
                continue
            for anchor in ANCHOR_RE.findall(text):
                if anchor not in slugs:
                    problems.append(f"{relative}:{number}: dead in-page anchor #{anchor}")
            for target in LINK_RE.findall(text):
                if target.startswith(("http:", "https:", "mailto:", "#", "/")):
                    continue
                path_part = target.split("#", 1)[0]
                if not path_part or not path_part.endswith(LINKABLE_SUFFIXES):
                    continue
                if not (page.parent / path_part).is_file():
                    problems.append(f"{relative}:{number}: missing relative link target {target}")

    return problems


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--content-dir", default="docs/docs/content")
    parser.add_argument("--mkdocs-yml", default="docs/docs/mkdocs.yml")
    args = parser.parse_args()

    content_dir = pathlib.Path(args.content_dir)
    mkdocs_yml = pathlib.Path(args.mkdocs_yml)
    for path in (content_dir, mkdocs_yml):
        if not path.exists():
            print(f"error: {path} does not exist", file=sys.stderr)
            return 2

    problems = check(content_dir, mkdocs_yml)
    if problems:
        print(f"documentation check failed with {len(problems)} problem(s):")
        for problem in problems:
            print(f"  - {problem}")
        return 1

    print("documentation check passed: nav, anchors, and relative links are consistent")
    return 0


if __name__ == "__main__":
    sys.exit(main())
