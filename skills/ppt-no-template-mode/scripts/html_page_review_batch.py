#!/usr/bin/env python3
"""批量并发 review + rewrite 无模板页面（默认并发 4）。

review：每页 读 HTML → 把 ../assets/ 本地图内联成 data: URL → html_to_png(stateless 模式，传 HTML 字符串)
        拿 png_base64 → html_page_review 评审 → 落 page_xxx.review.md。
rewrite：needs_rewrite 的页 → 把「当前 HTML + review issues」塞进 html_page_generate 重出修正版 →
         覆盖 page_xxx.html → 重跑 lint。

复用 html_page_generate_batch 里的 _build_prompt / _generate_once / _lint（同目录），rewrite 走和生成同一套 API。

用法:
    python html_page_review_batch.py --deck <deck_dir> \
        --review-prompt prompts/html_review_prompt.md \
        --gen-prompt prompts/html_gen_no_template.md [--concurrency 4]

stdout 一行 JSON:
    {"status":"ok","page_count":N,"reviewed":[...],"rewritten":[...],"rewrite_failed":[...],"details":[...]}
"""

from __future__ import annotations

import argparse
import base64
import io
import json
import os
import re
import sys
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _tool_call import ToolCallError, call_tool  # noqa: E402
import html_page_generate_batch as _genmod  # noqa: E402
from html_page_generate_batch import _build_prompt, _generate_once, _lint  # noqa: E402

try:
    import load_env
    load_env.load()
except (ImportError, AttributeError):
    pass

DEFAULT_CONCURRENCY = 4
VISION_MAX_BYTES = 4_500_000   # 超过就缩图，留 html_page_review 的 5MB 余量
VISION_MAX_SIDE = 1600
# 页面 HTML 里引用本地图的相对路径 ../assets/<file>.<ext>；stateless html_to_png 不读沙盒，
# 必须先把这些图内联成 data: URL。匹配 <img src>、CSS url(...) 等所有出现处。
_ASSET_REF_RE = re.compile(r"\.\./assets/[A-Za-z0-9_./-]+\.(?:png|jpe?g|gif|webp|svg)", re.IGNORECASE)
_MIME_BY_EXT = {
    "png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg",
    "gif": "image/gif", "webp": "image/webp", "svg": "image/svg+xml",
}
ISSUE_CODES = (
    "OUT_OF_PAGE", "OVERLAP", "CLIPPED", "EMPTY_BLOCK", "WEAK_MOTIF",
    "MISSING_DISPLAY_ITEM", "UNAPPROVED_CONTENT", "UNKNOWN_DISPLAY_ID",
    "UNUSED_ASSET", "STRAY_PLACEHOLDER", "FAKE_IMAGE", "BAD_IMAGE_SRC",
    "MISSING_PICTURE", "UNAPPROVED_FONT", "FONT_ROLE_MISMATCH",
    "UNAPPROVED_COLOR", "OTHER",
)


def _inline_assets(html: str, deck_dir: str) -> str:
    """把 HTML 里所有 ../assets/<file> 的本地图引用替换成 data:<mime>;base64,... 内联。

    stateless html_to_png 把 HTML 当字符串渲染、碰不到沙盒文件，所以本地图必须先内联进来，
    否则远端渲染出来是裂图。http(s)/data:/# 引用不在此正则范围内，原样保留。
    """
    assets_dir = Path(deck_dir) / "assets"
    for ref in set(_ASSET_REF_RE.findall(html)):
        fname = ref.split("../assets/", 1)[1]
        fpath = assets_dir / fname
        if not fpath.is_file():
            continue  # 缺图就保留原样（截图里会缺这张，但不阻塞评审）
        ext = fpath.suffix.lower().lstrip(".")
        mime = _MIME_BY_EXT.get(ext, "application/octet-stream")
        b64 = base64.b64encode(fpath.read_bytes()).decode("ascii")
        html = html.replace(ref, f"data:{mime};base64,{b64}")
    return html


def _guard_b64(b64: str) -> str:
    """html_to_png 返回的 png_base64 过大时，decode + 缩图重编码，保住 html_page_review 的 5MB 上限。"""
    raw = base64.b64decode(b64)
    if len(raw) <= VISION_MAX_BYTES:
        return b64
    from PIL import Image

    img = Image.open(io.BytesIO(raw)).convert("RGB")
    w, h = img.size
    scale = min(1.0, VISION_MAX_SIDE / max(w, h))
    if scale < 1.0:
        img = img.resize((max(1, int(w * scale)), max(1, int(h * scale))))
    buf = io.BytesIO()
    img.save(buf, format="JPEG", quality=85)
    return base64.b64encode(buf.getvalue()).decode("ascii")


def _parse_review(review_text: str) -> dict:
    """html_page_review 返回的 review 文本应是一段 JSON。解析按三级递进：
    ① 去 fence 后严格 json.loads；② 抽取首个 {...} 块再 loads（应对前后带说明文字）；
    ③ 字段级正则抽取 is_ok / needs_rewrite / issues（应对"JSON 但字符串内嵌未转义引号"的畸形输出，实测常见）。
    三级全失败才标 parse_ok=False → review_failed——**绝不**把解析失败当作通过（fail-closed）。"""
    text = (review_text or "").strip()
    if text.startswith("```"):
        text = text.split("```", 2)[1] if text.count("```") >= 2 else text
        text = text.split("\n", 1)[-1] if text.lower().startswith("json") else text
    data = None
    try:
        data = json.loads(text)
    except Exception:  # noqa: BLE001
        m = re.search(r"\{.*\}", text, re.S)
        if m:
            try:
                data = json.loads(m.group(0))
            except Exception:  # noqa: BLE001
                data = None
    if data is None:
        m_ok = re.search(r'"is_ok"\s*:\s*(true|false)', text)
        m_nr = re.search(r'"needs_rewrite"\s*:\s*(true|false)', text)
        if m_ok or m_nr:
            issues = re.findall(r'"(\[[A-Z_]{3,}\][^"]*)"', text)
            is_ok = (m_ok.group(1) == "true") if m_ok else not issues
            needs_rewrite = (m_nr.group(1) == "true") if m_nr else (bool(issues) and not is_ok)
            return {
                "is_ok": is_ok, "score": None, "needs_rewrite": needs_rewrite,
                "issues": issues,
                "suggestion": "（评审输出为畸形 JSON，已按字段抽取）" + text[:400],
                "parse_ok": True,
            }
        return {
            "is_ok": False, "score": None, "needs_rewrite": False,
            "issues": [], "suggestion": "（评审输出非 JSON，无法解析，已标记复核失败）" + text[:600],
            "parse_ok": False,
        }
    issues = data.get("issues") or []
    if not isinstance(issues, list):
        issues = [str(issues)]
    return {
        "is_ok": bool(data.get("is_ok", not issues)),
        "score": data.get("score"),
        "needs_rewrite": bool(data.get("needs_rewrite", bool(issues))),
        "issues": [str(x) for x in issues],
        "suggestion": str(data.get("suggestion", "")),
        "parse_ok": True,
    }


def _write_review_md(review_md_path: str, page_id: str, parsed: dict) -> None:
    issues = parsed["issues"] or []
    issue_block = "\n".join(f"- {x}" for x in issues) if issues else "无"
    md = (
        f"# {page_id} review\n\n"
        f"- mode: screenshot\n"
        f"- is_ok: {'true' if parsed['is_ok'] else 'false'}\n"
        f"- score: {parsed['score'] if parsed['score'] is not None else 'NA'}\n"
        f"- needs_rewrite: {'true' if parsed['needs_rewrite'] else 'false'}\n\n"
        f"## issues\n\n{issue_block}\n\n"
        f"## suggestion\n\n{parsed['suggestion'] or '无需修改'}\n"
    )
    Path(review_md_path).write_text(md, encoding="utf-8")


def _review_page(slice_path: Path, review_prompt: str) -> dict:
    detail = {"page": None, "page_id": None, "needs_rewrite": False, "issues": [],
              "slice_path": str(slice_path), "reason": ""}
    try:
        slice_obj = json.loads(slice_path.read_text(encoding="utf-8"))
    except Exception as exc:  # noqa: BLE001
        detail["reason"] = f"slice_unreadable({type(exc).__name__})"
        return detail
    page_number = slice_obj.get("page_number")
    page_id = slice_obj.get("page_id") or f"page_{int(page_number):03d}"
    html_path = slice_obj.get("output_html_path")
    deck_dir = slice_obj.get("deck_dir") or str(Path(html_path).parents[1])
    review_md_path = str(Path(deck_dir) / "htmls" / f"{page_id}.review.md")
    detail.update({"page": page_number, "page_id": page_id})

    if not html_path or not Path(html_path).is_file():
        detail["reason"] = "html_missing"
        return detail
    try:
        html = _inline_assets(Path(html_path).read_text(encoding="utf-8"), deck_dir)
        png = call_tool(
            "html_to_png",
            {"mode": "stateless", "html_file_content": html},
            tool_call_id=f"call_html_to_png_{page_id}",
        )
        b64 = png.get("png_base64")
        if not b64:
            raise ToolCallError(f"html_to_png returned no png_base64: {str(png)[:120]}")
        review = call_tool(
            "html_page_review",
            {"prompt": review_prompt, "image_base64": _guard_b64(b64)},
            tool_call_id=f"call_html_page_review_{page_id}",
        )
    except Exception as exc:  # noqa: BLE001
        detail["reason"] = f"review_fail({type(exc).__name__}: {exc})"[:200]
        return detail

    parsed = _parse_review(review.get("review", ""))
    _write_review_md(review_md_path, page_id, parsed)
    if not parsed.get("parse_ok", True):
        # 评审输出非 JSON → 无法判定该页是否合格，绝不当作"通过"静默跳过 rewrite；落成复核失败，由主 agent 据此收口
        detail["review_failed"] = True
        detail["reason"] = "review_parse_failed: 评审输出非 JSON，无法判定该页是否需改写"
        return detail
    detail["needs_rewrite"] = parsed["needs_rewrite"]
    detail["issues"] = parsed["issues"]
    return detail


def _rewrite_page(detail: dict, gen_prompt: str, deck_dir: str) -> dict:
    out = {"page": detail["page"], "page_id": detail["page_id"], "lint_ok": False, "reason": ""}
    slice_obj = json.loads(Path(detail["slice_path"]).read_text(encoding="utf-8"))
    html_path = slice_obj.get("output_html_path")
    page_id = detail["page_id"]
    try:
        current = Path(html_path).read_text(encoding="utf-8")
        extra = (
            "## 当前 HTML（在此基础上做最小修正，保持其余不变）\n```html\n"
            + current + "\n```\n\n## 必须修正的问题（来自页面截图评审）\n- "
            + "\n- ".join(detail["issues"][:20])
        )
        prompt = _build_prompt(gen_prompt, slice_obj, "no_template", extra=extra)
        fixed = _generate_once(prompt, page_id)
        Path(html_path).write_text(fixed, encoding="utf-8")
    except Exception as exc:  # noqa: BLE001
        out["reason"] = f"rewrite_generate_fail({type(exc).__name__})"[:200]
        return out
    lint_ok, reasons = _lint("no_template", deck_dir, detail["page"], html_path, None)
    out["lint_ok"] = lint_ok
    if not lint_ok:
        out["reason"] = ("lint_fail: " + "; ".join(str(r) for r in reasons[:8]))[:200]
    return out


def run(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--deck", required=True)
    parser.add_argument("--review-prompt", required=True)
    parser.add_argument("--gen-prompt", required=True, help="rewrite 复用的生成 prompt 模板")
    parser.add_argument("--concurrency", type=int, default=DEFAULT_CONCURRENCY)
    parser.add_argument("--canvas", default="1280x720",
                        help="画布尺寸 WxH，需与生成阶段一致；rewrite 的 prompt 注入与 lint 校验按此执行")
    args = parser.parse_args(argv)

    if args.canvas and "x" in args.canvas:
        _genmod.CANVAS = args.canvas.lower().strip()

    deck_dir = Path(args.deck)
    htmls_dir = deck_dir / "htmls"
    if not htmls_dir.is_dir():
        print(json.dumps({"status": "fail", "reason": f"htmls dir not found: {htmls_dir}"}))
        return 1
    review_prompt = Path(args.review_prompt).read_text(encoding="utf-8")
    gen_prompt = Path(args.gen_prompt).read_text(encoding="utf-8")
    slice_paths = sorted(htmls_dir.glob("page_*.input.json"))
    if not slice_paths:
        print(json.dumps({"status": "fail", "reason": "no page_*.input.json slices"}))
        return 1

    workers = max(1, args.concurrency)
    with ThreadPoolExecutor(max_workers=workers) as pool:
        details = list(pool.map(lambda p: _review_page(p, review_prompt), slice_paths))

    to_rewrite = [d for d in details if d.get("needs_rewrite") and d.get("issues")]
    rewrites: list[dict] = []
    if to_rewrite:
        with ThreadPoolExecutor(max_workers=workers) as pool:
            rewrites = list(pool.map(lambda d: _rewrite_page(d, gen_prompt, str(deck_dir)), to_rewrite))

    reviewed = [d["page"] for d in details if not d.get("reason")]
    review_failed = [d["page"] for d in details if d.get("reason")]
    rewritten = [r["page"] for r in rewrites if r["lint_ok"]]
    rewrite_failed = [r["page"] for r in rewrites if not r["lint_ok"]]
    print(json.dumps({
        "status": "ok",
        "page_count": len(details),
        "reviewed": reviewed,
        "review_failed": review_failed,
        "needs_rewrite": [d["page"] for d in to_rewrite],
        "rewritten": rewritten,
        "rewrite_failed": rewrite_failed,
        "details": details,
        "rewrites": rewrites,
    }, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    from _stage_status import cli_value, run_main
    raise SystemExit(run_main(run, cli_value("--deck"), "reviewing_pages", "review_and_rewrite_html"))
