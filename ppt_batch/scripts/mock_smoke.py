#!/usr/bin/env python3
"""Cross-platform PPT lifecycle smoke using deterministic main/subtask mocks."""

import json
import shutil
import subprocess
import sys
import time
from pathlib import Path


REPO = Path(__file__).resolve().parent.parent.parent
RUNNER = REPO / "ppt_batch" / "scripts" / "run_batch.py"
RUN_ROOT = REPO / "ppt_batch" / "runs"
WORKSPACE_ROOT = REPO / "workspace" / "result"
CASE_ID = "prod_26016eba"


def main():
    run_name = f"_smoke_ppt_{int(time.time() * 1000)}"
    run_dir = RUN_ROOT / run_name
    workspace_dir = WORKSPACE_ROOT / run_name
    command = [
        sys.executable,
        str(RUNNER),
        "--model", "ppt-mock",
        "--cases", CASE_ID,
        "--timeout", "90",
        "--out", run_name,
    ]
    try:
        subprocess.run(command, cwd=REPO, check=True)
        session_dir = run_dir / "sessions" / CASE_ID
        session_file = session_dir / f"{CASE_ID}.json"
        session = json.loads(session_file.read_text(encoding="utf-8"))
        transcript = "\n".join(str(message.get("content", "")) for message in session.get("messages", []))
        if "<ppt_task_finished>" not in transcript:
            raise AssertionError("main session did not contain ppt_task_finished")
        subtasks = sorted(
            path for path in (session_dir / "subtasks").glob("*.json")
            if path.name != "usage.json"
        )
        if not subtasks:
            raise AssertionError("main session did not create a subtask session")
        subtask = json.loads(subtasks[0].read_text(encoding="utf-8"))
        subtask_text = "\n".join(str(message.get("content", "")) for message in subtask.get("messages", []))
        if "<subtask_result>" not in subtask_text:
            raise AssertionError("subtask session did not contain subtask_result")
        print(f"PPT Mock smoke passed: main session + {len(subtasks)} subtask session(s)")
    finally:
        shutil.rmtree(run_dir, ignore_errors=True)
        shutil.rmtree(workspace_dir, ignore_errors=True)


if __name__ == "__main__":
    main()
