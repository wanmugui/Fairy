#!/usr/bin/env python3
"""Generate a compact outline.md through the configured tool-call API."""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import tempfile
from pathlib import Path
from typing import Any

from _stage_status import emit
from _tool_call import call_tool
from outline_md_preflight import metadata_header, parse_md, parse_text

DEFAULT_TOOL_NAME = "html_page_generate"


def strip_fence(text: str) -> str:
    text = text.strip()
    match = re.fullmatch(
        r"```(?:markdown|md)?\s*\n([\s\S]*?)\n```", text, re.IGNORECASE
    )
    return (match.group(1) if match else text).strip() + "\n"


def extract_text(result: Any) -> str:
    if isinstance(result, str):
        return result
    if not isinstance(result, dict):
        return ""
    for key in ("markdown", "text", "content", "html"):
        value = result.get(key)
        if isinstance(value, str) and value.strip():
            return value
    return ""


def render_outline(text: str, deck: Path, language: str, page_count: int) -> str:
    lines = strip_fence(text).splitlines()
    if lines and lines[0].startswith("<!--"):
        lines = lines[1:]
    header = metadata_header(deck, language, page_count)
    return f"{header}\n" + "\n".join(lines).lstrip("\n") + "\n"


def atomic_write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(
        dir=path.parent,
        prefix=f".{path.name}.",
        suffix=".tmp",
        text=True,
    )
    temporary_path = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            handle.write(content)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary_path, path)
    finally:
        if temporary_path.exists():
            temporary_path.unlink()


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--deck", required=True)
    parser.add_argument("--prompt")
    parser.add_argument("--force", action="store_true")
    parser.add_argument(
        "--tool-name",
        default=os.getenv("PPT_OUTLINE_TOOL_NAME", DEFAULT_TOOL_NAME),
        help="tool-call backend name; defaults to the existing text-capable HTML generator",
    )
    args = parser.parse_args()

    deck = Path(args.deck).resolve()
    output_path = deck / "outline.md"
    emit(deck, "drafting_outline", "generate_outline_md", "start")

    if output_path.is_file() and not args.force:
        parsed, errors, _warnings = parse_md(output_path)
        if not errors:
            page_count = len(parsed["pages"])
            emit(
                deck,
                "drafting_outline",
                "generate_outline_md",
                "done",
                progress={"completed": page_count, "failed": 0, "total": page_count},
            )
            print(
                json.dumps(
                    {
                        "status": "ok",
                        "outline_md": str(output_path),
                        "page_count": page_count,
                        "reused": True,
                    },
                    ensure_ascii=False,
                )
            )
            return 0

    try:
        task_pack = json.loads((deck / "task_pack.json").read_text(encoding="utf-8"))
        info_pack = json.loads((deck / "info_pack.json").read_text(encoding="utf-8"))
        prompt_path = (
            Path(args.prompt)
            if args.prompt
            else Path(__file__).parent.parent / "prompts" / "outline_md.md"
        )
        prompt_template = prompt_path.read_text(encoding="utf-8")
        prompt = prompt_template.replace(
            "{{TASK_PACK_JSON}}",
            json.dumps(task_pack, ensure_ascii=False),
        ).replace(
            "{{INFO_PACK_JSON}}",
            json.dumps(info_pack, ensure_ascii=False),
        )
        language = str(task_pack.get("language") or "zh")
        validation_errors: list[str] = []

        for attempt in (1, 2):
            correction = ""
            if validation_errors:
                correction = "\n上次输出结构错误，请修正：\n- " + "\n- ".join(
                    validation_errors
                )
            result = call_tool(
                args.tool_name,
                {"prompt": prompt + correction},
                tool_call_id=f"call_outline_md_{deck.name}_{attempt}",
            )
            candidate = strip_fence(extract_text(result))
            parsed, validation_errors, _warnings = parse_text(candidate)
            if validation_errors:
                continue

            page_count = len(parsed["pages"])
            atomic_write(
                output_path, render_outline(candidate, deck, language, page_count)
            )
            emit(
                deck,
                "drafting_outline",
                "generate_outline_md",
                "done",
                progress={"completed": page_count, "failed": 0, "total": page_count},
            )
            print(
                json.dumps(
                    {
                        "status": "ok",
                        "outline_md": str(output_path),
                        "page_count": page_count,
                        "reused": False,
                        "tool_name": args.tool_name,
                    },
                    ensure_ascii=False,
                )
            )
            return 0
        raise ValueError("; ".join(validation_errors))
    except Exception as exc:
        reason = str(exc)[:200] or type(exc).__name__
        emit(deck, "drafting_outline", "generate_outline_md", "failed", reason=reason)
        print(json.dumps({"status": "fail", "reason": str(exc)}, ensure_ascii=False))
        return 1


if __name__ == "__main__":
    sys.exit(main())
