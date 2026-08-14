#!/usr/bin/env python3
"""Pack preflight checker.

Used by the ppt-maker main agent to verify that task_pack.json or
info_pack.json actually exists and is well-formed before marking the
corresponding todolist task as complete. The main agent only has to
invoke this script and inspect exit_code + the one-line JSON output; it
must not open the JSON files itself.

Usage:
    python pack_preflight.py --check task_pack --deck <deck_dir>
    python pack_preflight.py --check info_pack --deck <deck_dir>
    python pack_preflight.py --check all       --deck <deck_dir>

Exit codes:
    0  ok
    2  missing / unreadable / invalid JSON / required field missing

Output (always one line of JSON on stdout):
    {"status": "ok", "check": "task_pack", "path": "...", ...}
    {"status": "fail", "check": "task_pack", "reason": "...", "missing": [...]}
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
    "schema_version",
    "deck_dir",
    "user_query",
    "topic",
    "language",
    "language_decision",
    "ppt_mode",
    "mode",
    "page_count_desc",
    "speaker_identity",
    "audience_identity",
    "use_case",
    "template_params",
    "content_gap_assessment",
    "supplemental_info_tasks",
    "visual_strategy",
]

VALID_VISUAL_STRATEGIES = ("chart_dominant", "image_dominant", "mixed")

VALID_LANGUAGE_DECISION_SOURCES = ("explicit_requirement", "query_fallback")
REQUIRED_LANGUAGE_DECISION_FIELDS = ("source", "matched_signal", "chosen_language", "note")

# ppt_mode 合法值及其到模式 Skill 的固定映射（契约见 references/task_pack.md）
MODE_BY_PPT_MODE = {
    "no-template": "ppt-no-template-mode",
    "template": "ppt-template-mode",
    "creative": "ppt-creative-mode",
}

REQUIRED_INFO_PACK_FIELDS = [
    "schema_version",
    "deck_dir",
    "user_input_summary",
    "content_blocks",
    "info_gaps",
]


def _fail(check: str, reason: str, **extra) -> int:
    payload = {"status": "fail", "check": check, "reason": reason, **extra}
    print(json.dumps(payload, ensure_ascii=False))
    return 2


def _ok(check: str, **extra) -> int:
    payload = {"status": "ok", "check": check, **extra}
    print(json.dumps(payload, ensure_ascii=False))
    return 0


def _check_json_file(path: Path, required_fields: list[str]) -> tuple[bool, str, list[str]]:
    if not path.is_file():
        return False, f"file not found: {path}", []
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        return False, f"invalid JSON: {exc}", []
    if not isinstance(data, dict):
        return False, "top-level JSON must be an object", []
    missing = [key for key in required_fields if key not in data]
    if missing:
        return False, "missing required fields", missing
    return True, "", []


def _check_language_decision(data: dict, path: Path) -> int | None:
    """校验 language_decision 留痕的结构自洽（方案 A）。

    只校验模型已结构化产出的判定痕迹本身，不对原始输入做任何 NLP/正则匹配：
    缺字段 / source 非法 / chosen_language 与 language 不一致 / source 与
    matched_signal 空否不匹配，均判 fail。返回 None 表示通过。
    """
    ld = data.get("language_decision")
    if not isinstance(ld, dict):
        return _fail(
            "task_pack", "language_decision must be an object",
            path=str(path), missing=["language_decision"],
        )
    ld_missing = [k for k in REQUIRED_LANGUAGE_DECISION_FIELDS if k not in ld]
    if ld_missing:
        return _fail(
            "task_pack", "language_decision missing sub-fields",
            path=str(path), missing=[f"language_decision.{k}" for k in ld_missing],
        )
    source = ld.get("source")
    if source not in VALID_LANGUAGE_DECISION_SOURCES:
        return _fail(
            "task_pack",
            f"language_decision.source must be one of {VALID_LANGUAGE_DECISION_SOURCES}, got {source!r}",
            path=str(path), missing=["language_decision.source"],
        )
    if ld.get("chosen_language") != data.get("language"):
        return _fail(
            "task_pack",
            f"language_decision.chosen_language ({ld.get('chosen_language')!r}) "
            f"must equal language ({data.get('language')!r})",
            path=str(path), missing=["language_decision.chosen_language"],
        )
    matched = ld.get("matched_signal")
    if not isinstance(matched, str):
        return _fail(
            "task_pack", "language_decision.matched_signal must be a string",
            path=str(path), missing=["language_decision.matched_signal"],
        )
    if source == "explicit_requirement" and not matched.strip():
        return _fail(
            "task_pack",
            "language_decision.matched_signal required when source=explicit_requirement",
            path=str(path), missing=["language_decision.matched_signal"],
        )
    if source == "query_fallback" and matched.strip():
        return _fail(
            "task_pack",
            "language_decision.matched_signal must be empty when source=query_fallback",
            path=str(path), missing=["language_decision.matched_signal"],
        )
    return None


def check_task_pack(deck_dir: Path) -> int:
    path = deck_dir / "task_pack.json"
    ok, reason, missing = _check_json_file(path, REQUIRED_TASK_PACK_FIELDS)
    if not ok:
        return _fail("task_pack", reason, path=str(path), missing=missing)
    data = json.loads(path.read_text(encoding="utf-8"))
    ld_rc = _check_language_decision(data, path)
    if ld_rc is not None:
        return ld_rc
    visual_strategy = data.get("visual_strategy")
    if visual_strategy not in VALID_VISUAL_STRATEGIES:
        return _fail(
            "task_pack",
            f"visual_strategy must be one of {VALID_VISUAL_STRATEGIES}",
            path=str(path),
            missing=["visual_strategy"],
        )
    ppt_mode = data.get("ppt_mode")
    if ppt_mode not in MODE_BY_PPT_MODE:
        return _fail(
            "task_pack",
            f"ppt_mode must be one of {tuple(MODE_BY_PPT_MODE)}, got {ppt_mode!r}",
            path=str(path),
            missing=["ppt_mode"],
        )
    expected_mode = MODE_BY_PPT_MODE[ppt_mode]
    if data.get("mode") != expected_mode:
        return _fail(
            "task_pack",
            f"mode must be {expected_mode!r} when ppt_mode={ppt_mode!r}, got {data.get('mode')!r}",
            path=str(path),
            missing=["mode"],
        )
    template_params = data.get("template_params") or {}
    if ppt_mode == "template":
        for key in ("template_name", "template_tags_path", "template_html_dir"):
            if not template_params.get(key):
                return _fail(
                    "task_pack",
                    f"template mode requires template_params.{key}",
                    path=str(path),
                    missing=[f"template_params.{key}"],
                )
        tags_path = Path(template_params["template_tags_path"])
        html_dir = Path(template_params["template_html_dir"])
        if not tags_path.is_file():
            return _fail(
                "task_pack",
                "template_tags_path does not exist",
                path=str(path),
                missing=[str(tags_path)],
            )
        if not html_dir.is_dir():
            return _fail(
                "task_pack",
                "template_html_dir does not exist",
                path=str(path),
                missing=[str(html_dir)],
            )
    return _ok("task_pack", path=str(path), ppt_mode=ppt_mode)


def check_info_pack(deck_dir: Path) -> int:
    path = deck_dir / "info_pack.json"
    ok, reason, missing = _check_json_file(path, REQUIRED_INFO_PACK_FIELDS)
    if not ok:
        return _fail("info_pack", reason, path=str(path), missing=missing)
    return _ok("info_pack", path=str(path))


def main() -> int:
    parser = argparse.ArgumentParser(description="ppt-maker pack preflight checker")
    parser.add_argument("--check", required=True, choices=["task_pack", "info_pack", "all"])
    parser.add_argument("--deck", required=True, help="absolute deck working directory")
    args = parser.parse_args()

    deck_dir = Path(args.deck)
    if not deck_dir.is_dir():
        return _fail(args.check, "deck directory does not exist", path=str(deck_dir))

    if args.check == "task_pack":
        return check_task_pack(deck_dir)
    if args.check == "info_pack":
        return check_info_pack(deck_dir)
    # all
    rc = check_task_pack(deck_dir)
    if rc != 0:
        return rc
    return check_info_pack(deck_dir)


if __name__ == "__main__":
    from _stage_status import cli_value, run_main
    sys.exit(run_main(main, cli_value("--deck"), "preparing_inputs", "validate_input_packs"))
