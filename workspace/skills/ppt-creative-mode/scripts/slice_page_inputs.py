#!/usr/bin/env python3
"""为 ppt-creative-mode 阶段 4 的每一页生成独立的输入切片。

主 agent 在进入阶段 4 之前调用本脚本；脚本消费 deck 工作目录中已存在的
`style_spec.md` / `outline.json`，为每一页产出 `pages/page_xxx.input.json`，
让 page-render subagent 只读自己这一页的切片，组装 API payload 并调
`creative_page_render.py` 出图。

使用方式：
    python slice_page_inputs.py --deck <deck_dir>

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


DEFAULT_TEMPLATE_ID = "default_template"

# 进入 API 的 page_outline 的字段（按 spec §4.3.1 映射表）
_API_FIELDS_DIRECT = ("title", "page_type", "layout", "content")


def _load_json(path: Path) -> dict:
    if not path.is_file():
        raise SystemExit(f"[slice_page_inputs] missing file: {path}")
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise SystemExit(f"[slice_page_inputs] invalid json {path}: {exc}") from exc


def _map_needed_pictures(items: list[dict] | None) -> list[dict]:
    """outline 新 schema {id, tag, purpose, size_hint} → API 旧格式 {id, caption, tag, size}."""
    if not items:
        return []
    mapped: list[dict] = []
    for it in items:
        if not isinstance(it, dict):
            continue
        mapped.append(
            {
                "id": it.get("id", ""),
                "caption": it.get("purpose", ""),
                "tag": it.get("tag", ""),
                "size": it.get("size_hint", ""),
            }
        )
    return mapped


def _build_page_outline(page: dict) -> dict:
    """按 spec §4.3.1 映射表装配 API 消费的 page_outline dict。"""
    out = {key: page.get(key) for key in _API_FIELDS_DIRECT}

    # template_id（公共 schema 中只在 template 模式为必填；creative 模式兜底为 default_template）
    out["template_id"] = page.get("template_id") or DEFAULT_TEMPLATE_ID

    # page_number_label (str) → API 的 page_number (str)；整数 page_number 不进 API dict
    page_number_label = page.get("page_number_label")
    if page_number_label:
        out["page_number"] = page_number_label

    # needed_pictures：新 schema → 旧 API 格式的字段映射
    out["needed_pictures"] = _map_needed_pictures(page.get("needed_pictures"))

    # 默认值兜底
    if out.get("layout") is None:
        out["layout"] = {}
    if out.get("content") is None:
        out["content"] = {}

    return out


def main() -> int:
    parser = argparse.ArgumentParser(description="Slice creative mode page inputs")
    parser.add_argument("--deck", required=True, help="deck working directory")
    args = parser.parse_args()

    deck_dir = Path(args.deck).resolve()
    if not deck_dir.is_dir():
        raise SystemExit(f"[slice_page_inputs] deck dir not found: {deck_dir}")

    style_md_path = deck_dir / "style_spec.md"
    if not style_md_path.is_file():
        raise SystemExit(f"[slice_page_inputs] missing file: {style_md_path}")
    style_md = style_md_path.read_text(encoding="utf-8")

    outline = _load_json(deck_dir / "outline.json")
    pages = outline.get("pages") or []
    if not pages:
        raise SystemExit("[slice_page_inputs] outline.pages is empty")

    ppt_id = deck_dir.name
    # 新公共 schema 顶层为 `title`；兼容历史产物仍用 `ppt_title`。
    ppt_title = outline.get("title") or outline.get("ppt_title") or ""

    pages_dir = deck_dir / "pages"
    pages_dir.mkdir(parents=True, exist_ok=True)

    sorted_pages = sorted(pages, key=lambda p: p.get("page_number", 0))

    outputs: list[dict] = []
    for page in sorted_pages:
        page_number = page.get("page_number")
        if not isinstance(page_number, int) or page_number <= 0:
            raise SystemExit(
                f"[slice_page_inputs] invalid page_number in outline: {page!r}"
            )
        page_tag = f"page_{page_number:03d}"
        output_png_path = pages_dir / f"{page_tag}.png"
        input_path = pages_dir / f"{page_tag}.input.json"

        slice_obj = {
            "schema_version": "1",
            "deck_dir": str(deck_dir),
            "page_number": page_number,
            "page_id": page.get("page_id") or page_tag,
            "output_png_path": str(output_png_path),
            "ppt_id": ppt_id,
            "ppt_title": ppt_title,
            "page_style_md": style_md,
            "page_outline": _build_page_outline(page),
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
                "output_png_path": str(output_png_path),
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
