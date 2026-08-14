#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
PPT skill batch runner (headless effect+cost testing).

For each case in tests/ppt_batch_cases/cases.json:
  - pre-creates a fixed local deck directory (workspace/result/<run>/<case>/)
  - builds a user message = <ppt_config> (logical /mnt/data/result/<run>/<case>/) + original query
  - resolves/builds the current-platform Agent with -AutoAnswerAskUser so outline confirms are
    auto-answered (no human in the loop)
  - collects session + usage.json for cost accounting

Usage:
  node scripts/python.mjs ppt_batch/scripts/run_batch.py --mode no-template --model clotho-qn-claude-opus-47 --limit 3
  pnpm test:ppt:mock
  node scripts/python.mjs ppt_batch/scripts/run_batch.py --collect-only --out ppt_batch_xxx   # regen CSV only
"""
import argparse, json, os, shutil, subprocess, sys, time, tempfile, re
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent.parent
AGENT_PATH_HELPER = REPO / "scripts" / "agent_path.mjs"
MANIFEST = REPO / "ppt_batch" / "cases" / "cases.json"
RUN_ROOT = REPO / "ppt_batch" / "runs"
# The local runtime maps the production workspace root /mnt/data to workspace/.
# Keep physical artifacts in workspace/result and send the Skill the logical
# production path, rather than leaking a host path into <ppt_config>.
WORKSPACE_ROOT = REPO / "workspace" / "result"
LOGICAL_DECK_ROOT = "/mnt/data/result"

# \u escape constants (survive any shell encoding)
S_NOT_SPEC = "\u672a\u6307\u5b9a"        # 未指定
S_ABOUT10 = "\u7ea610\u9875"             # 第10页
S_PRESENTER = "\u6f14\u793a\u8005"       # 演示者
S_AUDIENCE = "\u76ee\u6807\u53d7\u4f17"  # 目标受众
S_GEN_SCENE = "\u666e\u901a\u6c47\u62a5" # 普通汇报
S_PURE = "\u7eaf\u6587\u672c"            # 纯文本
S_OK = "OK"
S_FAIL = "FAIL"
S_TIMEOUT = "TIMEOUT"

MODEL_CONFIGS = {
    "minimax-text-01": {"cfg": "config/config.minimax.json", "override": None, "mock": False},
    "deepseek-chat":   {"cfg": "config/config.deepseek.json", "override": None, "mock": False},
    "mock":                     {"cfg": "config/config.json", "override": None, "mock": True},
    "ppt-mock":                 {"cfg": "config/config.json", "override": None, "mock": True, "mock_file": "ppt_batch/fixtures/mock_subtask_responses.json"},
}

_agent_path = None

def log(msg):
    print(f"[{time.strftime('%H:%M:%S')}] {msg}", flush=True)

def build_ppt_config(case, logical_deck_dir, mode):
    scene = (case.get("scene") or "").strip() or S_GEN_SCENE
    pages = (case.get("pages") or "").strip()
    pages_desc = pages if (pages and pages != S_NOT_SPEC) else S_ABOUT10
    return (
        "<ppt_config>\n"
        f"<role>{S_PRESENTER}</role>\n"
        f"<scene>{scene}</scene>\n"
        f"<audience>{S_AUDIENCE}</audience>\n"
        f"<page_count_desc>{pages_desc}</page_count_desc>\n"
        f"<ppt_mode>{mode}</ppt_mode>\n"
        f"<deck_dir>{logical_deck_dir}</deck_dir>\n"
        "</ppt_config>"
    )

def resolve_config(model_id):
    m = MODEL_CONFIGS[model_id]
    cfg_path = REPO / m["cfg"]
    tmp_cfg = None
    if m["override"] or m.get("mock_file"):
        data = json.loads(cfg_path.read_text(encoding="utf-8-sig"))
        if m["override"]:
            data.setdefault("api", {})["model"] = m["override"]
        if m.get("mock_file"):
            data["mock_file"] = m["mock_file"]
            data["use_mock"] = True
        tmp_cfg = REPO / ".tools" / f"cfg_override_{int(time.time()*1000)}.json"
        tmp_cfg.parent.mkdir(parents=True, exist_ok=True)
        tmp_cfg.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")
        cfg_path = tmp_cfg
    return cfg_path, m["mock"], tmp_cfg

def resolve_agent():
    """Use the same platform-aware Agent resolver as `pnpm dev`.

    The Node helper honors AGENT_LOOP_PATH and otherwise performs an incremental
    local Go build in .tools/. That keeps batch execution independent of an
    agent-loop.exe checked into the repository.
    """
    global _agent_path
    if _agent_path:
        return _agent_path
    if not AGENT_PATH_HELPER.is_file():
        raise RuntimeError(f"agent path helper not found: {AGENT_PATH_HELPER}")
    try:
        result = subprocess.run(
            ["node", str(AGENT_PATH_HELPER)], cwd=str(REPO),
            stdout=subprocess.PIPE, stderr=None, text=True, check=True,
        )
    except FileNotFoundError as exc:
        raise RuntimeError("Node.js is required to resolve the Agent; install Node.js and run pnpm dev once") from exc
    except subprocess.CalledProcessError as exc:
        raise RuntimeError(f"could not prepare current-platform Agent (exit {exc.returncode})") from exc
    candidates = [line.strip() for line in result.stdout.splitlines() if line.strip()]
    if not candidates:
        raise RuntimeError("Agent resolver returned no path")
    agent_path = Path(candidates[-1])
    if not agent_path.is_file():
        raise RuntimeError(f"Agent resolver returned a missing file: {agent_path}")
    _agent_path = agent_path
    return _agent_path

def _scan_msgs_usage(msgs):
    """Sum LLM calls + tokens from assistant messages carrying usage."""
    calls = 0
    pt = ct = dur = 0
    for m in msgs:
        if m.get("role") == "assistant":
            if m.get("usage"):
                calls += 1
                pt += m["usage"].get("prompt_tokens", 0) or 0
                ct += m["usage"].get("completion_tokens", 0) or 0
            if m.get("duration_ms"):
                dur += m["duration_ms"] or 0
    return calls, pt, ct, dur


def _scan_retries(msgs):
    """Count failed tool executions (tool results carrying error/ok:false)."""
    n = 0
    for m in msgs:
        if m.get("role") != "tool":
            continue
        c = m.get("content") or ""
        try:
            obj = json.loads(c)
            if isinstance(obj, dict) and (obj.get("error") or obj.get("ok") is False):
                n += 1
        except Exception:
            pass
    return n


def _scan_page_metrics(sdir, deck_dir_used):
    """Page-level metrics: expected vs actual pages, page-render LLM time, page retries.

    - expected: outline.json pages count (fallback: number of page_*.input.json)
    - actual:   pages/*.png (creative) or htmls/*.html (no-template/template)
    - page_llm_dur_ms: LLM time of per-page render subtasks (title contains "第 N 页")
    - page_retry_count: pages rendered more than once (duplicate page subtask = retry)
    """
    expected = 0
    actual = 0
    if deck_dir_used:
        d = Path(deck_dir_used)
        od = d / "outline.json"
        if od.exists():
            try:
                ol = json.loads(od.read_text(encoding="utf-8"))
                expected = len(ol.get("pages", []))
            except Exception:
                pass
        if not expected:
            try:
                expected = len(list((d / "pages").glob("page_*.input.json")))
            except Exception:
                pass
        if not expected:
            try:
                expected = len(list((d / "htmls").glob("page_*.input.json")))
            except Exception:
                pass
        png = d / "pages"
        htm = d / "htmls"
        if png.is_dir():
            actual = len(list(png.glob("*.png")))
        elif htm.is_dir():
            actual = len(list(htm.glob("*.html")))
    page_llm_dur = 0
    page_counts = {}
    subdir = sdir / "subtasks"
    if subdir.is_dir():
        for sf in subdir.glob("*.json"):
            if sf.name == "usage.json":
                continue
            m = re.search(r"\u7b2c(\d+)\u9875", sf.name)  # ?N?
            if m:
                pno = int(m.group(1))
                page_counts[pno] = page_counts.get(pno, 0) + 1
                try:
                    sess = json.loads(sf.read_text(encoding="utf-8"))
                except Exception:
                    continue
                page_llm_dur += sum((mm.get("duration_ms") or 0) for mm in sess.get("messages", []) if mm.get("duration_ms"))
    page_retries = sum(v - 1 for v in page_counts.values() if v > 1)
    return expected, actual, page_llm_dur, page_retries


def read_usage(session_file):
    """Aggregate cost data for one case from session json + usage.json.

    llm_call_count / tokens include the MAIN loop AND every subtask session
    (create_subtask spawns child agents that persist under <sess>/subtasks/).
    Subtasks typically consume MORE tokens than the main loop (outline, asset
    planning, per-page render), so ignoring them undercounts cost heavily.
    """
    sdir = Path(session_file).parent
    usage = {}
    uf = sdir / "usage.json"
    if uf.exists():
        try:
            usage = json.loads(uf.read_text(encoding="utf-8"))
        except Exception:
            usage = {}
    # main loop: count from session messages (authoritative), fallback usage.json
    main_calls = main_pt = main_ct = main_dur = 0
    retry_count = 0
    ppt_finished = False
    deck_dir_used = ""
    if Path(session_file).exists():
        try:
            sess = json.loads(Path(session_file).read_text(encoding="utf-8"))
            msgs = sess.get("messages", [])
            main_calls, main_pt, main_ct, main_dur = _scan_msgs_usage(msgs)
            retry_count += _scan_retries(msgs)
            all_text = "\n".join((m.get("content") or "") for m in msgs)
            ppt_finished = "<ppt_task_finished>" in all_text
            # Prefer the real deck_dir recorded inside tool results (JSON field),
            # e.g. "deck_dir": "D:\...\ppt_deck\pptid_xxx". The <deck_dir> tag
            # also appears inside skill-instruction text (placeholders), so only
            # fall back to tag matches that look like a real path.
            for mm in re.finditer(r'"deck_dir"\s*:\s*"([^"]+)"', all_text):
                deck_dir_used = mm.group(1)
            if not deck_dir_used:
                for mm in re.finditer(r"<deck_dir>\s*([^<`\s]+)", all_text):
                    cand = mm.group(1)
                    if re.match(r"^[A-Za-z]:[\\/]", cand) or "/" in cand:
                        deck_dir_used = cand
                        break
        except Exception:
            pass
    if main_calls == 0:
        main_pt = usage.get("prompt_tokens", 0)
        main_ct = usage.get("completion_tokens", 0)
        main_dur = usage.get("duration_ms", 0)
    # subtasks: every *.json under <sess>/subtasks/ (excluding usage.json etc.)
    sub_calls = sub_pt = sub_ct = sub_dur = 0
    subdir = sdir / "subtasks"
    if subdir.is_dir():
        for sf in sorted(subdir.glob("*.json")):
            if sf.name == "usage.json":
                continue
            try:
                sess = json.loads(sf.read_text(encoding="utf-8"))
            except Exception:
                continue
            c, pt, ct, dur = _scan_msgs_usage(sess.get("messages", []))
            sub_calls += c; sub_pt += pt; sub_ct += ct; sub_dur += dur
            retry_count += _scan_retries(sess.get("messages", []))
    page_expected, page_actual, page_llm_dur, page_retry_count = _scan_page_metrics(sdir, deck_dir_used)
    page_failure_rate = round((page_expected - page_actual) / page_expected, 4) if page_expected else 0.0
    return {
        "prompt_tokens": main_pt + sub_pt,
        "completion_tokens": main_ct + sub_ct,
        "duration_ms": (main_dur or usage.get("duration_ms", 0)) + sub_dur,
        "real_ms": usage.get("real_ms", 0),
        "llm_call_count": main_calls + sub_calls,
        "main_llm_calls": main_calls,
        "subtask_llm_calls": sub_calls,
        "retry_count": retry_count,
        "page_expected": page_expected,
        "page_actual": page_actual,
        "page_failure_rate": page_failure_rate,
        "page_llm_dur_ms": page_llm_dur,
        "page_retry_count": page_retry_count,
        "ppt_finished": ppt_finished,
        "deck_dir": deck_dir_used,
    }

def run_case(case, mode, model_id, run_dir, timeout_sec):
    cid = case["id"]
    deck_dir = WORKSPACE_ROOT / run_dir.name / cid
    logical_deck_dir = f"{LOGICAL_DECK_ROOT}/{run_dir.name}/{cid}"
    deck_dir.mkdir(parents=True, exist_ok=True)
    sess_dir = RUN_ROOT / run_dir.name / "sessions" / cid
    sess_dir.mkdir(parents=True, exist_ok=True)
    session_file = sess_dir / f"{cid}.json"

    user_msg = build_ppt_config(case, logical_deck_dir, mode) + "\n\n" + (case.get("prompt") or "")
    msg_file = run_dir / f"{cid}_user.txt"
    msg_file.write_text(user_msg, encoding="utf-8")
    logfile = run_dir / f"{cid}.log"
    logf = logfile.open("w", encoding="utf-8")

    cfg_path, use_mock, tmp_cfg = resolve_config(model_id)
    cmd = [
        str(resolve_agent()),
        "-ConfigPath", str(cfg_path),
        "-UseMock", "true" if use_mock else "false",
        "-UserOverrideFile", str(msg_file),
        "-SessionFile", str(session_file),
        "-AutoAnswerAskUser", "true",
    ]
    log(f"[{cid}] spawn: {model_id} mode={mode} mock={use_mock}")
    log(f"[{cid}] deck_dir={logical_deck_dir} -> {deck_dir}")
    start = time.time()
    auto_answers = []
    import threading
    def drain():
        try:
            for line in proc.stdout:
                logf.write(line)
                logf.flush()
                if "auto_answered" in line:
                    auto_answers.append(line.strip())
        except Exception:
            pass
    try:
        proc = subprocess.Popen(cmd, cwd=str(REPO), stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, encoding="utf-8", errors="replace")
        th = threading.Thread(target=drain, daemon=True)
        th.start()
        try:
            proc.wait(timeout=timeout_sec)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
            rc = -1
            log(f"[{cid}] TIMEOUT after {timeout_sec}s")
        else:
            rc = proc.returncode
        th.join(timeout=5)
    except Exception:
        rc = -1
    finally:
        logf.close()
        if tmp_cfg:
            try: tmp_cfg.unlink()
            except Exception: pass

    elapsed = time.time() - start
    data = read_usage(session_file)
    status = S_OK if (rc == 0 and data.get("ppt_finished")) else (S_TIMEOUT if rc == -1 else S_FAIL)
    if rc == 0 and not data.get("ppt_finished"):
        # finished without explicit ppt_task_finished tag (e.g. no-template may vary)
        if data.get("llm_call_count", 0) > 0:
            status = S_OK + "(no-tag)"
    log(f"[{cid}] rc={rc} status={status} elapsed={elapsed:.0f}s calls={data.get('llm_call_count')} prompt={data.get('prompt_tokens')} completion={data.get('completion_tokens')} auto_answered={len(auto_answers)}")
    return {
        "case": cid,
        "status": status,
        "model": model_id,
        "mode": mode,
        "elapsed_s": round(elapsed, 1),
        "rc": rc,
        "auto_answered": len(auto_answers),
        "deck_dir": str(deck_dir),
        **data,
    }

def write_csv(run_dir, rows):
    cols = ["case", "status", "model", "mode", "elapsed_s", "rc", "auto_answered",
            "prompt_tokens", "completion_tokens", "duration_ms", "real_ms",
            "llm_call_count", "main_llm_calls", "subtask_llm_calls",
            "retry_count", "page_expected", "page_actual", "page_failure_rate",
            "page_llm_dur_ms", "page_retry_count", "ppt_finished", "deck_dir"]
    csv_path = run_dir / "costs.csv"
    with csv_path.open("w", encoding="utf-8-sig", newline="") as f:
        f.write(",".join(cols) + "\n")
        for r in rows:
            f.write(",".join(str(r.get(c, "")) for c in cols) + "\n")
    return csv_path

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--mode", default="no-template", choices=["no-template", "template", "creative"])
    ap.add_argument("--model", default="minimax-text-01", choices=list(MODEL_CONFIGS.keys()))
    ap.add_argument("--cases", default="", help="comma-separated case ids; default = all pure-text cases")
    ap.add_argument("--limit", type=int, default=0, help="max cases to run (0=all)")
    ap.add_argument("--out", default="", help="run dir name (default runs/ppt_batch_<ts>)")
    ap.add_argument("--timeout", type=int, default=45*60, help="per-case timeout seconds")
    ap.add_argument("--collect-only", action="store_true", help="only regenerate CSV from an existing run dir")
    ap.add_argument("--all-cases", action="store_true", help="include cases with attachments too")
    args = ap.parse_args()

    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))

    if args.collect_only:
        run_dir = RUN_ROOT / args.out
        rows = []
        # legacy runs used "ppt_batch_<run>" session dirs; new runs use "<run>"
        sess_dirs = set(run_dir.glob("sessions/*"))
        for cid_dir in sorted(sess_dirs):
            if not cid_dir.is_dir():
                continue
            cid = cid_dir.name
            sess = cid_dir / f"{cid}.json"
            data = read_usage(sess)
            rows.append({"case": cid, "model": args.model, "mode": args.mode, **data})
        write_csv(run_dir, rows)
        log(f"CSV regenerated: {run_dir / 'costs.csv'}")
        return

    ts = time.strftime("%Y%m%d-%H%M%S")
    run_name = args.out or f"ppt_batch_{ts}"
    run_dir = RUN_ROOT / run_name
    run_dir.mkdir(parents=True, exist_ok=True)

    if args.cases:
        wanted = [c.strip() for c in args.cases.split(",") if c.strip()]
        by_id = {case["id"]: case for case in manifest}
        missing = [case_id for case_id in wanted if case_id not in by_id]
        if missing:
            ap.error("unknown case id(s): " + ", ".join(missing))
        # A caller usually orders --cases deliberately (for example, a quick
        # low-page smoke before a longer deck), so do not silently reorder it
        # to the manifest's source order.
        cases = [by_id[case_id] for case_id in wanted]
    else:
        cases = [c for c in manifest if S_PURE in (c.get("input_type") or "")]
        if args.all_cases:
            cases = manifest
    if args.limit:
        cases = cases[:args.limit]
    log(f"plan: {len(cases)} cases, mode={args.mode}, model={args.model}, timeout={args.timeout}s")

    rows = []
    for i, case in enumerate(cases, 1):
        log(f"--- [{i}/{len(cases)}] {case['id']}")
        rows.append(run_case(case, args.mode, args.model, run_dir, args.timeout))
    csv_path = write_csv(run_dir, rows)
    summary = {
        "run": run_name,
        "mode": args.mode,
        "model": args.model,
        "total_cases": len(rows),
        "ok": sum(1 for r in rows if r["status"].startswith(S_OK)),
        "fail": sum(1 for r in rows if not r["status"].startswith(S_OK)),
        "total_prompt_tokens": sum(r["prompt_tokens"] for r in rows),
        "total_completion_tokens": sum(r["completion_tokens"] for r in rows),
        "total_calls": sum(r["llm_call_count"] for r in rows),
    }
    (run_dir / "summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
    log("=== SUMMARY ===")
    log(json.dumps(summary, ensure_ascii=False, indent=2))
    log(f"costs csv: {csv_path}")

if __name__ == "__main__":
    main()
