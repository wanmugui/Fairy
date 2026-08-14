#!/usr/bin/env python3
"""Validate restricted outline.md and strict outline.json parity."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any

from _stage_status import emit

TYPE_MAP = {
    "封面": "title",
    "目录": "catalog",
    "过渡": "transition",
    "内容": "content",
    "总结": "summary",
    "结尾": "ending",
    "title": "title",
    "catalog": "catalog",
    "transition": "transition",
    "content": "content",
    "summary": "summary",
    "ending": "ending",
}
PAGE_RE = re.compile(r"^##\s+(\d+)\.\s+(.+?)\s+\[([^\]]+)\]\s*$")
ITEM_RE = re.compile(r"^(\s*)-\s+(.+?)\s*$")
META_RE = re.compile(
    r"^<!-- ppt-outline v1 \| deck: ([^|]+) \| lang: ([^|]+) \| pages: (\d+) -->$"
)
MAX_POINT_LENGTH = 80


def parse_text(text: str) -> tuple[dict[str, Any], list[str], list[str]]:
    """Parse the restricted Markdown subset without mutating the source."""
    errors: list[str] = []
    warnings: list[str] = []
    title: str | None = None
    pages: list[dict[str, Any]] = []
    metadata: dict[str, Any] | None = None
    current_page: dict[str, Any] | None = None
    current_point: dict[str, Any] | None = None

    lines = text.splitlines()
    for line_number, raw_line in enumerate(lines, 1):
        line = raw_line.rstrip()
        if not line:
            continue
        if line_number == 1 and line.startswith("<!--"):
            match = META_RE.fullmatch(line)
            if match:
                metadata = {
                    "deck_id": match.group(1).strip(),
                    "lang": match.group(2).strip(),
                    "pages": int(match.group(3)),
                }
            else:
                warnings.append("metadata header is invalid and will be rebuilt")
            continue
        if line.startswith("<!--"):
            errors.append(
                f"line {line_number}: comments are only allowed in the metadata header"
            )
            continue
        if line.startswith("# ") and not line.startswith("## "):
            if title is not None:
                errors.append(f"line {line_number}: duplicate deck title")
            title = line[2:].strip()
            if not title:
                errors.append(f"line {line_number}: empty deck title")
            continue

        page_match = PAGE_RE.fullmatch(line)
        if page_match:
            page_type_token = page_match.group(3).strip()
            page_type = TYPE_MAP.get(page_type_token)
            if page_type is None:
                errors.append(
                    f"line {line_number}: invalid page type {page_type_token!r}"
                )
            page_title = page_match.group(2).strip()
            if not page_title:
                errors.append(f"line {line_number}: empty page title")
            current_page = {
                "number": int(page_match.group(1)),
                "title": page_title,
                "type": page_type,
                "points": [],
            }
            pages.append(current_page)
            current_point = None
            continue

        item_match = ITEM_RE.fullmatch(line)
        if item_match:
            if current_page is None:
                errors.append(f"line {line_number}: item appears before any page")
                continue
            indentation = len(item_match.group(1).replace("\t", "  "))
            name = item_match.group(2).strip()
            if not name:
                errors.append(f"line {line_number}: empty point")
            if len(name) > MAX_POINT_LENGTH:
                errors.append(
                    f"line {line_number}: point exceeds {MAX_POINT_LENGTH} characters"
                )
            if indentation == 0:
                current_point = {"name": name, "children": []}
                current_page["points"].append(current_point)
            elif indentation == 2 and current_point is not None:
                current_point["children"].append(name)
            else:
                errors.append(
                    f"line {line_number}: only two-space second-level indentation is allowed"
                )
            continue

        errors.append(f"line {line_number}: unsupported Markdown element")

    if title is None:
        errors.append("missing single '# deck title'")
    if not pages:
        errors.append("outline contains no pages")
    page_numbers = [page["number"] for page in pages]
    if page_numbers != list(range(1, len(pages) + 1)):
        errors.append(f"page numbers must be continuous 1..N, got {page_numbers}")

    for page in pages:
        point_count = len(page["points"])
        if page["type"] == "content" and not 1 <= point_count <= 6:
            errors.append(
                f"page {page['number']}: content page must contain 1..6 points"
            )
        if page["type"] == "ending" and point_count > 2:
            errors.append(
                f"page {page['number']}: ending page must contain at most 2 points"
            )

    return {"metadata": metadata, "title": title, "pages": pages}, errors, warnings


def parse_md(path: Path) -> tuple[dict[str, Any], list[str], list[str]]:
    if not path.is_file():
        return {}, [f"outline.md not found: {path}"], []
    return parse_text(path.read_text(encoding="utf-8"))


def metadata_header(deck: Path, language: str, page_count: int) -> str:
    return f"<!-- ppt-outline v1 | deck: {deck.name} | lang: {language} | pages: {page_count} -->"


def normalize_metadata(path: Path, parsed: dict[str, Any], language: str) -> None:
    """Rebuild the first-line metadata after user edits."""
    lines = path.read_text(encoding="utf-8").splitlines()
    if lines and lines[0].startswith("<!--"):
        lines = lines[1:]
    header = metadata_header(path.parent, language, len(parsed.get("pages", [])))
    path.write_text(
        f"{header}\n" + "\n".join(lines).lstrip("\n") + "\n", encoding="utf-8"
    )


def json_points(page: dict[str, Any]) -> list[dict[str, Any]]:
    content = page.get("content") or {}
    values = content.values() if isinstance(content, dict) else content
    if not isinstance(values, (list, tuple)) and not hasattr(values, "__iter__"):
        return []
    result: list[dict[str, Any]] = []
    for point in values:
        if not isinstance(point, dict):
            continue
        children = point.get("sub_sub_points") or {}
        child_values = children.values() if isinstance(children, dict) else children
        if not isinstance(child_values, (list, tuple)) and not hasattr(
            child_values, "__iter__"
        ):
            child_values = []
        result.append(
            {
                "name": point.get("sub_point_name"),
                "children": [
                    child.get("sub_sub_point_name")
                    for child in child_values
                    if isinstance(child, dict)
                ],
            }
        )
    return result


def compare(parsed: dict[str, Any], outline: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    if parsed.get("title") != outline.get("title"):
        errors.append("deck title differs")
    markdown_pages = parsed.get("pages") or []
    json_pages = outline.get("pages") or []
    if len(markdown_pages) != len(json_pages):
        errors.append(
            f"page count differs: md={len(markdown_pages)} json={len(json_pages)}"
        )
    for index, (markdown_page, json_page) in enumerate(
        zip(markdown_pages, json_pages), 1
    ):
        if json_page.get("page_number") != markdown_page["number"]:
            errors.append(f"page {index}: number differs")
        if json_page.get("page_type") != markdown_page["type"]:
            errors.append(f"page {index}: type differs")
        if json_page.get("title") != markdown_page["title"]:
            errors.append(f"page {index}: title differs")
        if json_points(json_page) != markdown_page["points"]:
            errors.append(f"page {index}: point hierarchy differs")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--deck", required=True)
    parser.add_argument("--against-json", action="store_true")
    args = parser.parse_args()

    deck = Path(args.deck).resolve()
    stage = "building_content" if args.against_json else "drafting_outline"
    activity = "validate_outline_parity" if args.against_json else "validate_outline_md"
    emit(deck, stage, activity, "start")
    parsed, errors, warnings = parse_md(deck / "outline.md")

    if not errors:
        language = "zh"
        try:
            task_pack = json.loads(
                (deck / "task_pack.json").read_text(encoding="utf-8")
            )
            language = str(task_pack.get("language") or language)
        except (OSError, json.JSONDecodeError):
            warnings.append("task_pack language unavailable; metadata uses zh")
        normalize_metadata(deck / "outline.md", parsed, language)

    if not errors and args.against_json:
        try:
            outline = json.loads((deck / "outline.json").read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            errors.append(f"outline.json unreadable: {exc}")
        else:
            errors.extend(compare(parsed, outline))

    status = "fail" if errors else "ok"
    emit(
        deck,
        stage,
        activity,
        "failed" if errors else "done",
        reason="; ".join(errors)[:200],
    )
    print(
        json.dumps(
            {
                "status": status,
                "check": "outline_md",
                "page_count": len(parsed.get("pages", [])),
                "errors": errors,
                "warnings": warnings,
            },
            ensure_ascii=False,
        )
    )
    return 2 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
