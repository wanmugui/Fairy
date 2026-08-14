#!/usr/bin/env python3
"""为无模板模式阶段 5/6/7 的每一页生成独立的输入切片。

主 agent 在进入阶段 5 之前调用本脚本；脚本消费 deck 工作目录中已存在的
`style_spec.json` / `outline.json` / `asset_map.json` / `task_pack.json`，
为每一页产出 `htmls/page_xxx.input.json`，让 html-generation / page-review-polish
subagent 只读自己这一页的切片，避免每个 page subtask 都读整份多页 JSON。

使用方式：
    python slice_stage5_inputs.py --deck <deck_dir>

成功时以 JSON 形式打印产出列表到 stdout（供主 agent 验收），失败时写 stderr 并
返回非零退出码。
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
        raise SystemExit(f"[slice_stage5_inputs] missing file: {path}")
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise SystemExit(f"[slice_stage5_inputs] invalid json {path}: {exc}") from exc


def index_pages(pages: list, label: str) -> dict:
    indexed: dict = {}
    for page in pages:
        page_number = page.get("page_number")
        if not isinstance(page_number, int):
            raise SystemExit(
                f"[slice_stage5_inputs] {label}: page missing integer page_number: {page!r}"
            )
        if page_number in indexed:
            raise SystemExit(
                f"[slice_stage5_inputs] {label}: duplicate page_number {page_number}"
            )
        indexed[page_number] = page
    return indexed


def main() -> int:
    parser = argparse.ArgumentParser(description="Slice stage5 inputs per page (no-template mode)")
    parser.add_argument("--deck", required=True, help="deck working directory")
    args = parser.parse_args()

    deck_dir = Path(args.deck).resolve()
    if not deck_dir.is_dir():
        raise SystemExit(f"[slice_stage5_inputs] deck dir not found: {deck_dir}")

    style_spec = load_json(deck_dir / "style_spec.json")
    outline = load_json(deck_dir / "outline.json")
    asset_map = load_json(deck_dir / "asset_map.json")

    ol_pages = index_pages(outline.get("pages") or [], "outline")
    am_pages = index_pages(asset_map.get("pages") or [], "asset_map")

    page_numbers = sorted(ol_pages)
    if not page_numbers:
        raise SystemExit("[slice_stage5_inputs] outline.pages is empty")
    if sorted(am_pages) != page_numbers:
        raise SystemExit(
            "[slice_stage5_inputs] page_number sets differ between outline and asset_map"
        )

    htmls_dir = deck_dir / "htmls"
    htmls_dir.mkdir(parents=True, exist_ok=True)

    outputs: list[dict] = []
    for page_number in page_numbers:
        page_tag = f"page_{page_number:03d}"
        output_html_path = htmls_dir / f"{page_tag}.html"
        input_path = htmls_dir / f"{page_tag}.input.json"

        slice_obj = {
            "schema_version": "1",
            "deck_dir": str(deck_dir),
            "page_number": page_number,
            "page_id": ol_pages[page_number].get("page_id") or page_tag,
            "output_html_path": str(output_html_path),
            "task_pack_path": str(deck_dir / "task_pack.json"),
            "style_spec": style_spec,
            "outline_page": ol_pages[page_number],
            "asset_map_page": am_pages[page_number],
        }

        input_path.write_text(
            json.dumps(slice_obj, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
        outputs.append(
            {
                "page_number": page_number,
                "page_tag": page_tag,
                "input_path": str(input_path),
                "output_html_path": str(output_html_path),
            }
        )

    print(
        json.dumps(
            {
                "deck_dir": str(deck_dir),
                "page_count": len(outputs),
                "pages": outputs,
            },
            ensure_ascii=False,
        )
    )
    return 0


if __name__ == "__main__":
    from _stage_status import cli_value, run_main
    sys.exit(run_main(main, cli_value("--deck"), "generating_pages", "prepare_page_inputs"))
