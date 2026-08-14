#!/usr/bin/env python3
"""从 tag_gen_results.json 向 template_map.json 补全脚本可派生的字段。

LLM 生成 slim template_map.json 后，由 subagent 调用本脚本，
把 template_page_type / layout_category / template_constraints /
template_raw_constraints 等字段从模板配置文件原样填入，
避免 LLM 花大量推理时间做简单的字段搬运。

使用方式（在 subtask 内执行）：
    python /mnt/data/skill/ppt-template-mode/scripts/enrich_template_map.py \
        --template-map <template_map.json> \
        --tags <tag_gen_results.json>

脚本会原地覆写 template_map.json。
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


def load_json(path: Path) -> dict:
    if not path.is_file():
        raise SystemExit(f"[enrich] file not found: {path}")
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise SystemExit(f"[enrich] invalid json {path}: {exc}") from exc


def enrich_page(page: dict, tag: dict) -> None:
    """Mutate *page* in-place, filling script-derivable fields from *tag*."""
    # template_page_type
    page["template_page_type"] = tag.get("page_type", "")

    # layout_category
    layout = tag.get("layout") or {}
    page["layout_category"] = layout.get("category", "custom")

    # template_constraints (subset of tag)
    page["template_constraints"] = {
        "image_num": tag.get("image_num", 0),
        "chart_num": tag.get("chart_num", 0),
        "table_num": tag.get("table_num", 0),
        "available_text_subpoint_range": tag.get("available_text_subpoint_range", [1, 4]),
        "available_text_subpoint_character_number": tag.get("available_text_subpoint_character_number", 200),
        "has_background_image": tag.get("has_background_image", False),
        "background_image_size": tag.get("background_image_size"),
    }

    # template_raw_constraints (full tag, excluding fallback_img for brevity)
    raw = dict(tag)
    raw.pop("fallback_img", None)
    page["template_raw_constraints"] = raw


def main() -> int:
    parser = argparse.ArgumentParser(description="Enrich slim template_map.json")
    parser.add_argument("--template-map", required=True, help="Path to template_map.json")
    parser.add_argument("--tags", required=True, help="Path to tag_gen_results.json")
    args = parser.parse_args()

    tm_path = Path(args.template_map)
    tags_path = Path(args.tags)

    tm = load_json(tm_path)
    tags = load_json(tags_path)

    pages = tm.get("pages")
    if not isinstance(pages, list):
        raise SystemExit("[enrich] template_map.json missing 'pages' array")

    enriched_count = 0
    for page in pages:
        tid = str(page.get("template_id", ""))
        if tid not in tags:
            print(f"[enrich] WARNING: template_id '{tid}' not found in tags, skipping page {page.get('page_number')}", file=sys.stderr)
            continue
        enrich_page(page, tags[tid])
        enriched_count += 1

    # Write enriched JSON first so the subagent can inspect the file even if validation fails below.
    tm_path.write_text(
        json.dumps(tm, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )

    # Hard validation: page_role must match the enriched template_page_type for strict roles.
    # This catches stage-2 subagents that select a content/catalog template for title/ending
    # roles and write a fabricated selection_reason like "使用真实ending模板".
    strict_role_to_page_type = {
        "title": "title",
        "ending": "ending",
        "catalog": "catalog",
        "transition": "transition",
    }
    mismatches = []
    for page in pages:
        role = page.get("page_role", "")
        expected = strict_role_to_page_type.get(role)
        if not expected:
            continue
        actual = page.get("template_page_type", "")
        if actual != expected:
            mismatches.append({
                "page_number": page.get("page_number"),
                "page_role": role,
                "template_id": str(page.get("template_id", "")),
                "template_page_type": actual,
                "expected_page_type": expected,
            })

    if mismatches:
        error_payload = {
            "status": "fail",
            "reason": "page_role/template_page_type mismatch after enrich",
            "mismatches": mismatches,
            "fix_hint": "重新为这些页选择 page_type 与 page_role 一致的模板候选，不得以 selection_reason 文案伪造",
        }
        print(json.dumps(error_payload, ensure_ascii=False), file=sys.stderr)
        return 2

    print(json.dumps({"enriched_pages": enriched_count, "total_pages": len(pages)}))
    return 0


if __name__ == "__main__":
    from _stage_status import cli_value, run_main
    _deck = Path(cli_value("--template-map")).resolve().parent
    sys.exit(run_main(main, _deck, "matching_templates", "enrich_template_map"))
