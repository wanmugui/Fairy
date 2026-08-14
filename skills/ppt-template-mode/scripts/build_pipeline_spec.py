#!/usr/bin/env python3
"""从 outline.json + 极简候选映射，自动拼出 image_pipeline 的 spec —— 省掉 LLM 手写整个 spec。

asset-planning subagent 原来要亲手把「18 槽 × 5 候选 URL + 每槽一段长 check_prompt + 各槽 asset_id/asset_kind/slot_desc」
写成一份大 spec JSON（实测耗时 ~3 分钟纯输出）。本脚本把这堆机械活全接管：

agent 只需提供 `candidates.json` = {"picture-001": ["url1","url2",...], ...}（槽 → 候选 URL 列表，URL 可本地路径），
本脚本从 outline.json 枚举图槽、命名 asset_id、按 outline 推导 slot_desc、用 check_prompt 模板生成每槽 check_prompt、
组装出 image_pipeline.py 吃的完整 spec。

只收录「需要新搜图」的槽位：`sub_point.picture` 非空且 `picture_source_ref` 为空，以及非空的 `background_picture`。
既有图复用（picture_source_ref 非空）不进 spec（走 cp）；candidates 里没有的槽也跳过（agent 没搜到就没传）。

用法:
    python build_pipeline_spec.py --outline <outline.json> --candidates <candidates.json> \
        --check-prompt <prompts/image_filter_check_prompt.md> --deck <deck_dir> \
        --out <deck_dir>/assets/_pipeline_spec.json [--concurrency 16]

stdout 一行 JSON: {"status":"ok","out":"...","slot_count":N,"slots":[...],"skipped_no_candidates":[...]}
退出码：成功 0；outline/candidates/模板不可读或非法 1。
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


def _subpoint_sort_key(key: str) -> int:
    digits = "".join(ch for ch in key if ch.isdigit())
    return int(digits) if digits else 0


def _enumerate_search_slots(outline: dict) -> list[dict]:
    """从 outline 枚举所有需要新搜图的槽位，带上 asset_id/asset_kind/slot_desc。"""
    deck_title = outline.get("title", "")
    slots: list[dict] = []
    for page in outline.get("pages", []):
        page_id = page.get("page_id") or ""
        page_title = page.get("title", "")
        ill_n = 0
        bg_n = 0

        bg = page.get("background_picture") or ""
        if bg:
            bg_n += 1
            slots.append({
                "asset_id": f"{page_id}_bg_{bg_n:02d}",
                "slot_id": bg,
                "page_id": page_id,
                "asset_kind": "background_image",
                "slot_desc": (page_title or deck_title or "整页底图").strip(),
            })

        content = page.get("content") or {}
        for key in sorted(content.keys(), key=_subpoint_sort_key):
            sp = content[key] or {}
            pic = sp.get("picture") or ""
            ref = sp.get("picture_source_ref") or ""
            if not pic or ref:  # 无图槽 或 既有图复用 —— 不进搜图 spec
                continue
            ill_n += 1
            name = sp.get("sub_point_name") or ""
            slot_desc = (f"{name} {page_title}".strip() or deck_title or "配图").strip()
            slots.append({
                "asset_id": f"{page_id}_ill_{ill_n:02d}",
                "slot_id": pic,
                "page_id": page_id,
                "asset_kind": "illustration",
                "slot_desc": slot_desc,
            })
    return slots


def run(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--outline", required=True)
    parser.add_argument("--candidates", required=True, help="{slot_id: [url,...]} 的 JSON 文件")
    parser.add_argument("--check-prompt", required=True, help="image_filter_check_prompt.md 路径")
    parser.add_argument("--deck", required=True)
    parser.add_argument("--out", default=None)
    parser.add_argument("--concurrency", type=int, default=16)
    args = parser.parse_args(argv)

    try:
        outline = json.loads(Path(args.outline).read_text(encoding="utf-8"))
        candidates = json.loads(Path(args.candidates).read_text(encoding="utf-8"))
        check_tpl = Path(args.check_prompt).read_text(encoding="utf-8")
    except Exception as exc:  # noqa: BLE001
        print(json.dumps({"status": "fail", "reason": f"read input failed: {type(exc).__name__}: {exc}"}))
        return 1
    if not isinstance(candidates, dict):
        print(json.dumps({"status": "fail", "reason": "candidates.json 必须是 {slot_id: [url,...]} 对象"}))
        return 1

    out_path = args.out or str(Path(args.deck) / "assets" / "_pipeline_spec.json")
    all_slots = _enumerate_search_slots(outline)

    spec_slots: list[dict] = []
    skipped: list[str] = []
    for s in all_slots:
        cand = candidates.get(s["slot_id"]) or []
        if not cand:
            skipped.append(s["slot_id"])
            continue
        spec_slots.append({
            "asset_id": s["asset_id"],
            "slot_id": s["slot_id"],
            "page_id": s["page_id"],
            "asset_kind": s["asset_kind"],
            "check_prompt": check_tpl.replace("{slot_desc}", s["slot_desc"]),
            "candidates": list(cand),
        })

    spec = {"deck_dir": args.deck, "concurrency": args.concurrency, "slots": spec_slots}
    Path(out_path).parent.mkdir(parents=True, exist_ok=True)
    Path(out_path).write_text(json.dumps(spec, ensure_ascii=False), encoding="utf-8")

    print(json.dumps({
        "status": "ok",
        "out": out_path,
        "slot_count": len(spec_slots),
        "slots": [s["slot_id"] for s in spec_slots],
        "skipped_no_candidates": skipped,
    }, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    from _stage_status import cli_value, run_main
    sys.exit(run_main(run, cli_value("--deck"), "preparing_assets", "build_asset_spec"))
