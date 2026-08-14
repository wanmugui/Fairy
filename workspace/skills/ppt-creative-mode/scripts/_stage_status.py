#!/usr/bin/env python3
"""Emit PPT_STAGE v1 events without making telemetry a workflow dependency."""

from __future__ import annotations

import contextlib
import json
import os
import sys
import tempfile
from pathlib import Path
from typing import Any, Callable

VALID_STAGES = {
    "preparing_inputs",
    "drafting_outline",
    "matching_templates",
    "building_content",
    "preparing_assets",
    "generating_pages",
    "reviewing_pages",
    "finalizing",
}
VALID_STATUSES = {"start", "done", "failed"}


def emit(
    deck_dir: str | Path,
    stage: str,
    activity: str,
    status: str,
    **fields: Any,
) -> dict[str, Any]:
    """Print an event and best-effort append it to stage_status.jsonl."""
    if stage not in VALID_STAGES:
        raise ValueError(f"invalid PPT stage: {stage}")
    if status not in VALID_STATUSES:
        raise ValueError(f"invalid PPT status: {status}")

    deck = Path(deck_dir).resolve()
    event: dict[str, Any] = {
        "v": 1,
        "deck_id": deck.name,
        "stage": stage,
        "activity": activity,
        "status": status,
    }
    event.update(
        {key: value for key, value in fields.items() if value not in (None, "")}
    )
    serialized = json.dumps(event, ensure_ascii=False, separators=(",", ":"))
    print(f"[PPT_STAGE] {serialized}", flush=True)

    if deck.is_dir():
        try:
            descriptor = os.open(
                deck / "stage_status.jsonl",
                os.O_APPEND | os.O_CREAT | os.O_WRONLY,
                0o644,
            )
            try:
                os.write(descriptor, f"{serialized}\n".encode("utf-8"))
            finally:
                os.close(descriptor)
        except OSError:
            # Telemetry must never prevent the presentation workflow from running.
            pass
    return event


def _last_json(output: str) -> dict[str, Any]:
    for line in reversed(output.splitlines()):
        try:
            value = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(value, dict):
            return value
    return {}


def _outcome(
    return_code: int, payload: dict[str, Any], activity: str = ""
) -> tuple[str, dict[str, int] | None, str]:
    failed_pages: list[Any] = []
    for key in ("failed_pages", "review_failed", "rewrite_failed"):
        values = payload.get(key) or []
        if isinstance(values, list):
            failed_pages.extend(value for value in values if value not in failed_pages)
    results = payload.get("results") or []
    failed_results = [
        item
        for item in results
        if isinstance(item, dict) and not item.get("local_path")
    ]
    failed = (
        return_code != 0
        or payload.get("status") in {"fail", "failed", "error"}
        or bool(failed_pages)
        or (bool(failed_results) and activity != "process_assets")
    )

    progress = None
    if isinstance(payload.get("page_count"), int):
        total = payload["page_count"]
        failed_count = len(failed_pages)
        progress = {
            "completed": total - failed_count,
            "failed": failed_count,
            "total": total,
        }
    elif isinstance(results, list) and results:
        total = len(results)
        failed_count = len(failed_results)
        progress = {
            "completed": total - failed_count,
            "failed": failed_count,
            "total": total,
        }

    reason = str(payload.get("reason") or "")
    if failed and not reason:
        reason = f"return_code={return_code}; failed_items={len(failed_pages) + len(failed_results)}"
    return ("failed" if failed else "done"), progress, reason[:200]


def run_main(
    main: Callable[[], int | None],
    deck_dir: str | Path,
    stage: str,
    activity: str,
) -> int:
    """Run a CLI main and preserve its last-line business JSON contract."""
    emit(deck_dir, stage, activity, "start")
    with tempfile.SpooledTemporaryFile(
        mode="w+",
        max_size=1024 * 1024,
        encoding="utf-8",
    ) as buffer:
        try:
            with contextlib.redirect_stdout(buffer):
                return_code = int(main() or 0)
        except BaseException as exc:
            emit(
                deck_dir,
                stage,
                activity,
                "failed",
                reason=str(exc)[:200] or type(exc).__name__,
            )
            raise

        buffer.seek(0)
        output = buffer.read()
    payload = _last_json(output)
    status, progress, reason = _outcome(return_code, payload, activity)
    emit(deck_dir, stage, activity, status, progress=progress, reason=reason)
    if output:
        print(output, end="" if output.endswith("\n") else "\n")
    return return_code


def cli_value(flag: str, default: str = ".") -> str:
    """Read either '--flag value' or '--flag=value' from sys.argv."""
    prefix = f"{flag}="
    for index, argument in enumerate(sys.argv):
        if argument.startswith(prefix):
            return argument[len(prefix) :]
        if argument == flag and index + 1 < len(sys.argv):
            return sys.argv[index + 1]
    return default
