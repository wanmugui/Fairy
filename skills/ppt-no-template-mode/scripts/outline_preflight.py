#!/usr/bin/env python3
"""Outline preflight checker（无模板模式 stage 3 确定性自检）。

把 `stages/03-outline-generation.md` 的跨字段自检项固化成确定性脚本，替代模型手写
Python 校验。outline 子任务**只跑这一个脚本**、读 stdout 一行 JSON：有 error 就一次性
整体重写 outline.json，再跑一次确认即可，不要多轮 edit_file 逐处改。

校验分两档：
  errors   —— 必须修的硬约束（schema / 跨字段一致性）；非空则 exit 2。
  warnings —— 软目标（配图分布、结构对称、软下限）；只提示不阻断，exit 仍为 0。

Usage:
    python outline_preflight.py --deck <deck_dir>

Output（stdout 恒为一行 JSON）:
    {"status":"ok","check":"outline","page_count":N,"errors":[],"warnings":[...]}
    {"status":"fail","check":"outline","page_count":N,"errors":[...],"warnings":[...]}

Exit codes:
    0  无 error（可能有 warning）
    2  有 error / 文件缺失 / JSON 非法
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

try:
    import load_env
    load_env.load()
except (ImportError, AttributeError):
    pass

VALID_PAGE_TYPES = {"title", "content", "catalog", "transition", "summary", "ending"}
VALID_BLOCK_TYPES = {"heading", "body_text", "bullet_list", "chart", "table", "image", "quote", "kpi"}
PIC_RE = re.compile(r"^picture-\d{3}$")
PAGE_ID_RE = re.compile(r"^page_\d{3}$")
SUBPOINT_KEY_RE = re.compile(r"^sub_point[1-9]\d*$")
SUBSUB_KEY_RE = re.compile(r"^sub_sub_point[1-9]\d*$")
REQUIRED_TOP_FIELDS = ["schema_version", "deck_dir", "title", "total_pages", "pages"]
MAX_BLOCKS_CAP = 6
ENDING_MAX = 2


def _emit(errors: list[str], warnings: list[str], page_count: int) -> int:
    status = "fail" if errors else "ok"
    print(json.dumps(
        {"status": status, "check": "outline", "page_count": page_count,
         "errors": errors, "warnings": warnings},
        ensure_ascii=False,
    ))
    return 2 if errors else 0


def _visual_count(sp: dict) -> int:
    """该 sub_point 是否含图片（picture 非空）；chart/table 另算。"""
    return 1 if (sp.get("picture") or "").strip() else 0


def main() -> int:
    parser = argparse.ArgumentParser(description="ppt-no-template outline preflight checker")
    parser.add_argument("--deck", required=True, help="absolute deck working directory")
    args = parser.parse_args()

    deck_dir = Path(args.deck)
    errors: list[str] = []
    warnings: list[str] = []

    path = deck_dir / "outline.json"
    if not path.is_file():
        return _emit([f"outline.json not found: {path}"], warnings, 0)
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        return _emit([f"invalid JSON: {exc}"], warnings, 0)
    if not isinstance(data, dict):
        return _emit(["top-level JSON must be an object"], warnings, 0)

    missing = [k for k in REQUIRED_TOP_FIELDS if k not in data]
    if missing:
        return _emit([f"missing top-level fields: {missing}"], warnings, 0)

    pages = data.get("pages")
    if not isinstance(pages, list) or not pages:
        return _emit(["pages must be a non-empty list"], warnings, 0)
    page_count = len(pages)

    total_pages = data.get("total_pages")
    if isinstance(total_pages, int) and total_pages != page_count:
        errors.append(f"len(pages)={page_count} != total_pages={total_pages}")

    # variant 词表（缺 style_spec 则降级为 warning，不阻断其它校验）
    variant_ids: set[str] | None = None
    sty = deck_dir / "style_spec.json"
    if sty.is_file():
        try:
            sdata = json.loads(sty.read_text(encoding="utf-8"))
            variant_ids = {v.get("variant_id") for v in (sdata.get("page_type_variants") or [])}
        except (json.JSONDecodeError, AttributeError):
            warnings.append("style_spec.json 解析失败，跳过 variant_id 校验")
    else:
        warnings.append("style_spec.json 不存在，跳过 variant_id 校验")

    seen_numbers: list[int] = []
    all_pics: list[str] = []
    deck_has_visual = False
    dist_counts: list[int] = []  # 需靠图片表达的 content 页的单页配图数（图表页豁免）

    for idx, p in enumerate(pages):
        tag = f"pages[{idx}]"
        if not isinstance(p, dict):
            errors.append(f"{tag} 不是对象")
            continue
        pid = p.get("page_id") or ""
        if not PAGE_ID_RE.match(pid):
            errors.append(f"{tag} page_id 非法（须 page_NNN）: {pid!r}")
        ptype = p.get("page_type")
        if ptype not in VALID_PAGE_TYPES:
            errors.append(f"{tag} page_type 非法: {ptype!r}")
        pn = p.get("page_number")
        if isinstance(pn, int):
            seen_numbers.append(pn)

        # variant 引用
        if variant_ids is not None:
            vid = p.get("page_type_variant_id")
            if vid not in variant_ids:
                errors.append(f"{tag} page_type_variant_id 不在 style_spec 中: {vid!r}")

        budget = p.get("content_budget") or {}
        max_blocks = budget.get("max_content_blocks")
        if isinstance(max_blocks, int) and max_blocks > MAX_BLOCKS_CAP:
            errors.append(f"{tag} max_content_blocks={max_blocks} 超过上限 {MAX_BLOCKS_CAP}")

        content = p.get("content")
        if not isinstance(content, dict) or not content:
            errors.append(f"{tag} content 必须为非空 dict（至少 1 个 sub_point）")
            content = {}

        # content key 形态 + 连续性
        keys = list(content.keys())
        bad_keys = [k for k in keys if not SUBPOINT_KEY_RE.match(k)]
        if bad_keys:
            errors.append(f"{tag} content 含非法 key（须 sub_pointN）: {bad_keys}")
        nums = sorted(int(k[len("sub_point"):]) for k in keys if SUBPOINT_KEY_RE.match(k))
        if nums and nums != list(range(1, len(nums) + 1)):
            errors.append(f"{tag} sub_point 编号不连续（须 1..N）: {nums}")

        # len(content) ≤ max_content_blocks
        if isinstance(max_blocks, int) and len(content) > max_blocks:
            errors.append(f"{tag} len(content)={len(content)} > max_content_blocks={max_blocks}")

        # 结尾页收敛（≤2 块）
        if ptype == "ending":
            if len(content) > ENDING_MAX:
                errors.append(f"{tag} ending 页 len(content)={len(content)} > {ENDING_MAX}（结尾只放总结性结论+致谢）")
            if isinstance(max_blocks, int) and max_blocks > ENDING_MAX:
                errors.append(f"{tag} ending 页 max_content_blocks={max_blocks} > {ENDING_MAX}")

        # 逐 sub_point
        page_pic = 0
        page_has_chart_table = False
        for k, sp in content.items():
            if not isinstance(sp, dict):
                errors.append(f"{tag}.{k} 不是对象")
                continue
            bt = sp.get("block_type")
            if bt is not None and bt not in VALID_BLOCK_TYPES:
                errors.append(f"{tag}.{k} block_type 非法: {bt!r}")
            tcp = [bool((sp.get(x) or "").strip()) for x in ("table", "chart", "picture")]
            if sum(tcp) > 1:
                errors.append(f"{tag}.{k} table/chart/picture 至多一个非空")
            pic = (sp.get("picture") or "").strip()
            if pic:
                if not PIC_RE.match(pic):
                    errors.append(f"{tag}.{k} picture 非 picture-NNN 格式: {pic!r}")
                all_pics.append(pic)
                page_pic += 1
                deck_has_visual = True
            if (sp.get("chart") or "").strip() or (sp.get("table") or "").strip():
                page_has_chart_table = True
                deck_has_visual = True
            psr = (sp.get("picture_source_ref") or "").strip()
            if psr and not pic:
                errors.append(f"{tag}.{k} picture_source_ref 非空但 picture 为空")
            ssp = sp.get("sub_sub_points")
            if ssp and isinstance(ssp, dict):
                bad_ss = [kk for kk in ssp if not SUBSUB_KEY_RE.match(kk)]
                if bad_ss:
                    errors.append(f"{tag}.{k} sub_sub_points 含非法 key: {bad_ss}")

        # background_picture
        bg = (p.get("background_picture") or "").strip()
        if bg:
            if not PIC_RE.match(bg):
                errors.append(f"{tag} background_picture 非 picture-NNN 格式: {bg!r}")
            all_pics.append(bg)
            deck_has_visual = True

        # require_image / require_chart_or_table 满足
        if budget.get("require_image") and page_pic == 0 and not bg:
            errors.append(f"{tag} require_image=true 但本页无 picture 也无 background_picture")
        if budget.get("require_chart_or_table") and not page_has_chart_table:
            errors.append(f"{tag} require_chart_or_table=true 但本页无 chart/table")

        # 配图分布分母（软）：content 页、且未用 chart/table 承载视觉的才计入
        if ptype == "content" and not page_has_chart_table:
            dist_counts.append(page_pic + (1 if bg else 0))

    # page_number 覆盖 1..N 唯一
    if isinstance(total_pages, int):
        expect = list(range(1, total_pages + 1))
    else:
        expect = list(range(1, page_count + 1))
    if sorted(seen_numbers) != expect:
        errors.append(f"page_number 须覆盖 {expect[0]}..{expect[-1]} 且唯一，实际={sorted(seen_numbers)}")

    # picture-NNN 全 deck 唯一
    dup = sorted({x for x in all_pics if all_pics.count(x) > 1})
    if dup:
        errors.append(f"picture-NNN 重复: {dup}")

    # deck 级硬下限：至少一处视觉
    if not deck_has_visual:
        errors.append("deck 级硬下限：全 deck 无任何 chart/table/picture/background_picture")

    # ---- 软目标（warnings）----
    n = len(dist_counts)
    if n >= 3:
        multi = sum(1 for c in dist_counts if c >= 2)
        if multi / n > 0.45:  # ~30% + 15% 容差
            warnings.append(
                f"配图分布偏多图：需靠图片表达的 content 页中 {multi}/{n} 为多图（建议多数收紧到 1 张）"
            )

    return _emit(errors, warnings, page_count)


if __name__ == "__main__":
    from _stage_status import cli_value, run_main
    sys.exit(run_main(main, cli_value("--deck"), "building_content", "validate_outline_json"))
