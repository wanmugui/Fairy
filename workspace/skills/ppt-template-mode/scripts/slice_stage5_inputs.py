#!/usr/bin/env python3
"""为阶段 5/6/7 的每一页生成独立的输入切片。

主 agent 在进入阶段 5 之前调用本脚本；脚本只消费 deck 工作目录中已存在的
`template_map.json` / `outline.json` / `asset_map.json` / `task_pack.json`，
为每一页产出 `htmls/page_xxx.input.json`，让 html-generation / page-review-polish
subagent 只读自己这一页的切片，避免每个 page subtask 都读整份 8 页 JSON。

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
    parser = argparse.ArgumentParser(description="Slice stage5 inputs per page")
    parser.add_argument("--deck", required=True, help="deck working directory")
    args = parser.parse_args()

    deck_dir = Path(args.deck).resolve()
    if not deck_dir.is_dir():
        raise SystemExit(f"[slice_stage5_inputs] deck dir not found: {deck_dir}")

    template_map = load_json(deck_dir / "template_map.json")
    outline = load_json(deck_dir / "outline.json")
    asset_map = load_json(deck_dir / "asset_map.json")
    task_pack = load_json(deck_dir / "task_pack.json")

    template_params = (task_pack or {}).get("template_params") or {}
    template_html_dir = template_params.get("template_html_dir")
    if not template_html_dir:
        raise SystemExit(
            "[slice_stage5_inputs] task_pack.json missing template_params.template_html_dir"
        )
    template_html_dir_path = Path(template_html_dir)
    if not template_html_dir_path.is_dir():
        raise SystemExit(
            f"[slice_stage5_inputs] template_html_dir not found: {template_html_dir_path}"
        )

    tm_pages = index_pages(template_map.get("pages") or [], "template_map")
    ol_pages = index_pages(outline.get("pages") or [], "outline")
    am_pages = index_pages(asset_map.get("pages") or [], "asset_map")

    page_numbers = sorted(tm_pages)
    if not page_numbers:
        raise SystemExit("[slice_stage5_inputs] template_map.pages is empty")
    if sorted(ol_pages) != page_numbers or sorted(am_pages) != page_numbers:
        raise SystemExit(
            "[slice_stage5_inputs] page_number sets differ across "
            "template_map / outline / asset_map"
        )

    htmls_dir = deck_dir / "htmls"
    htmls_dir.mkdir(parents=True, exist_ok=True)

    # 把模板目录下的静态资源目录（htmls_png / user / themes）复制到 deck 下，
    # 使模板 HTML 里的相对路径（如 ../htmls_png/cover.png、../themes/light.css）
    # 在 deck/htmls/ 下也能解析——themes/ 存主题色变量，生成的 HTML 会保留
    # `<link href="../themes/*.css">`，不带过来则主题链接悬空、主题切换失效。
    # 必须用复制而非软链接，因为结果从沙盒复制到输出目录时软链接会丢失。
    import shutil

    template_root = template_html_dir_path.parent  # e.g. templates/new-year-1
    for static_dir_name in ("htmls_png", "user", "themes"):
        src = template_root / static_dir_name
        dst = deck_dir / static_dir_name
        if src.is_dir() and not dst.exists():
            shutil.copytree(str(src), str(dst))

    outputs: list[dict] = []
    for page_number in page_numbers:
        tm_page = tm_pages[page_number]
        template_id = tm_page.get("template_id")
        if not template_id:
            raise SystemExit(
                f"[slice_stage5_inputs] template_map page {page_number} missing template_id"
            )
        template_html_path = template_html_dir_path / f"{template_id}.html"
        if not template_html_path.is_file():
            raise SystemExit(
                f"[slice_stage5_inputs] template html not found: {template_html_path}"
            )

        page_tag = f"page_{page_number:03d}"
        output_html_path = htmls_dir / f"{page_tag}.html"
        input_path = htmls_dir / f"{page_tag}.input.json"

        slice_obj = {
            "schema_version": "1",
            "deck_dir": str(deck_dir),
            "page_number": page_number,
            "page_id": tm_page.get("page_id") or page_tag,
            "template_id": template_id,
            "template_html_path": str(template_html_path),
            "output_html_path": str(output_html_path),
            "task_pack_path": str(deck_dir / "task_pack.json"),
            "template_map_page": tm_page,
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
