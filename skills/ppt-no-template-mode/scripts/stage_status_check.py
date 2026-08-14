#!/usr/bin/env python3
"""Validate a PPT_STAGE JSONL journal without backend support."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from _stage_status import VALID_STAGES, VALID_STATUSES


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--deck", required=True)
    args = parser.parse_args()

    deck = Path(args.deck).resolve()
    journal = deck / "stage_status.jsonl"
    errors: list[str] = []
    active: dict[tuple[str, str], int] = {}
    event_count = 0

    if not journal.is_file():
        errors.append(f"missing: {journal}")
    else:
        for line_number, line in enumerate(
            journal.read_text(encoding="utf-8").splitlines(), 1
        ):
            try:
                event = json.loads(line)
            except json.JSONDecodeError as exc:
                errors.append(f"line {line_number}: {exc}")
                continue
            event_count += 1
            if event.get("v") != 1:
                errors.append(f"line {line_number}: invalid v")
            if event.get("deck_id") != deck.name:
                errors.append(f"line {line_number}: deck_id differs")
            if event.get("stage") not in VALID_STAGES:
                errors.append(f"line {line_number}: invalid stage")
            if event.get("status") not in VALID_STATUSES:
                errors.append(f"line {line_number}: invalid status")
            if not event.get("activity"):
                errors.append(f"line {line_number}: missing activity")

            key = (str(event.get("stage")), str(event.get("activity")))
            status = event.get("status")
            if status == "start":
                active[key] = active.get(key, 0) + 1
            elif status in {"done", "failed"}:
                if active.get(key, 0) < 1:
                    errors.append(
                        f"line {line_number}: terminal event without start: {key}"
                    )
                else:
                    active[key] -= 1
                if status == "failed" and not event.get("reason"):
                    errors.append(f"line {line_number}: failed event missing reason")

        for key, pending in active.items():
            if pending:
                errors.append(f"unfinished activity: {key} x{pending}")

    print(
        json.dumps(
            {
                "status": "fail" if errors else "ok",
                "check": "ppt_stage",
                "event_count": event_count,
                "errors": errors,
            },
            ensure_ascii=False,
        )
    )
    return 2 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
