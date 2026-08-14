#!/usr/bin/env python3
"""ppt-template-mode preflight checker.

Verifies that every prerequisite for entering template mode is actually
satisfied on disk, so the main agent doesn't have to string together
multiple `test -f` calls (and can't silently proceed when a file it
claimed to create is missing). The main agent runs this exactly once
before dispatching the stage 2 subagent.

Usage:
    python preflight.py --deck <deck_dir>

Exit codes:
    0  all checks passed
    2  any check failed

Output (one line of JSON on stdout):
    ok   -> {"status":"ok", "deck_dir":..., "template_name":..., "template_tags_path":..., "template_html_dir":...}
    fail -> {"status":"fail", "reason":"...", "missing":[...]}
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

try:
    import load_env
    load_env.load()
except (ImportError, AttributeError):
    pass


REQUIRED_TASK_PACK_FIELDS = [
    "deck_dir",
    "ppt_mode",
    "mode",
    "template_params",
]

REQUIRED_INFO_PACK_FIELDS = [
    "deck_dir",
]


def _fail(reason: str, **extra) -> int:
    payload = {"status": "fail", "reason": reason, **extra}
    print(json.dumps(payload, ensure_ascii=False))
    return 2


def _ok(**extra) -> int:
    payload = {"status": "ok", **extra}
    print(json.dumps(payload, ensure_ascii=False))
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description="ppt-template-mode preflight checker")
    parser.add_argument("--deck", required=True, help="absolute deck working directory")
    args = parser.parse_args()

    deck_dir = Path(args.deck)
    if not deck_dir.is_dir():
        return _fail("deck directory does not exist", deck_dir=str(deck_dir))

    task_pack_path = deck_dir / "task_pack.json"
    info_pack_path = deck_dir / "info_pack.json"

    if not task_pack_path.is_file():
        return _fail("task_pack.json missing", missing=[str(task_pack_path)])
    if not info_pack_path.is_file():
        return _fail("info_pack.json missing", missing=[str(info_pack_path)])

    try:
        task_pack = json.loads(task_pack_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        return _fail(f"task_pack.json invalid JSON: {exc}", path=str(task_pack_path))
    try:
        info_pack = json.loads(info_pack_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        return _fail(f"info_pack.json invalid JSON: {exc}", path=str(info_pack_path))

    missing_fields: list[str] = []
    for key in REQUIRED_TASK_PACK_FIELDS:
        if key not in task_pack:
            missing_fields.append(f"task_pack.{key}")
    for key in REQUIRED_INFO_PACK_FIELDS:
        if key not in info_pack:
            missing_fields.append(f"info_pack.{key}")
    if missing_fields:
        return _fail("required fields missing", missing=missing_fields)

    if task_pack.get("ppt_mode") != "template":
        return _fail(
            "task_pack.ppt_mode must be 'template' for template mode",
            ppt_mode=task_pack.get("ppt_mode"),
        )
    if task_pack.get("mode") != "ppt-template-mode":
        return _fail(
            "task_pack.mode must be 'ppt-template-mode'",
            mode=task_pack.get("mode"),
        )

    template_params = task_pack.get("template_params") or {}
    template_name = template_params.get("template_name")
    template_tags_path = template_params.get("template_tags_path")
    template_html_dir = template_params.get("template_html_dir")
    missing_tp: list[str] = []
    if not template_name:
        missing_tp.append("template_params.template_name")
    if not template_tags_path:
        missing_tp.append("template_params.template_tags_path")
    if not template_html_dir:
        missing_tp.append("template_params.template_html_dir")
    if missing_tp:
        return _fail("template_params incomplete", missing=missing_tp)

    tags_file = Path(template_tags_path)
    html_dir = Path(template_html_dir)
    if not tags_file.is_file():
        return _fail(
            "template_tags_path does not point to a real file",
            missing=[str(tags_file)],
        )
    if not html_dir.is_dir():
        return _fail(
            "template_html_dir does not point to a real directory",
            missing=[str(html_dir)],
        )
    # At least one <template_id>.html must exist inside the directory
    if not any(html_dir.glob("*.html")):
        return _fail(
            "template_html_dir contains no <template_id>.html files",
            missing=[str(html_dir / "*.html")],
        )

    return _ok(
        deck_dir=str(deck_dir),
        template_name=template_name,
        template_tags_path=str(tags_file),
        template_html_dir=str(html_dir),
    )


if __name__ == "__main__":
    from _stage_status import cli_value, run_main
    sys.exit(run_main(main, cli_value("--deck"), "preparing_inputs", "validate_template_inputs"))
