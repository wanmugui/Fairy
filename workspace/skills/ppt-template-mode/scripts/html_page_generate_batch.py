#!/usr/bin/env python3
"""批量并发调用 html_page_generate API 出所有页 HTML（默认并发 4）。

把「subagent 亲手写 HTML」改成「组装 prompt → 调 html_page_generate → 落盘 → lint」。
一个生成子任务跑这一个脚本即可，脚本内部线程池并发处理所有页（不再每页一个子任务）。

流程（每页）:
  1. 读 htmls/page_xxx.input.json 切片。
  2. 组装 prompt = prompt 模板全文 + 本页数据(JSON) + （模板模式）参考模板 HTML 全文。
  3. POST html_page_generate，拿 {html}，去掉可能的 ```html 包裹，落盘 output_html_path。
  4. 跑 lint 自检；失败则把 lint reasons 追加进 prompt 重出一次；再失败标记该页 failed（HTML 仍保留）。

用法:
    python html_page_generate_batch.py --deck <deck_dir> --mode no_template --prompt <prompt.md> [--concurrency 4]
    python html_page_generate_batch.py --deck <deck_dir> --mode template    --prompt <prompt.md> [--concurrency 4]

stdout 一行 JSON:
    {"status":"ok","page_count":N,"ok_pages":[1,2],"failed_pages":[3],
     "details":[{"page":1,"output":"...","lint_ok":true,"retried":false,"reason":""}]}
退出码：全部页 lint 通过=0；有失败页=0（失败体现在 failed_pages，由主 agent 据此收口）；deck/切片不可读=1。
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _tool_call import ToolCallError, call_tool  # noqa: E402

try:
    import load_env
    load_env.load()
except (ImportError, AttributeError):
    pass

SCRIPT_DIR = Path(os.path.dirname(os.path.abspath(__file__)))
DEFAULT_CONCURRENCY = 4
_FENCE_RE = re.compile(r"^\s*```[a-zA-Z]*\s*|\s*```\s*$")


def _strip_fence(html: str) -> str:
    s = html.strip()
    if s.startswith("```"):
        s = re.sub(r"^\s*```[a-zA-Z]*\s*", "", s)
        s = re.sub(r"\s*```\s*$", "", s)
    return s.strip()


def _page_data(slice_obj: dict, mode: str) -> dict:
    common = {
        "page_number": slice_obj.get("page_number"),
        "page_id": slice_obj.get("page_id"),
        "outline_page": slice_obj.get("outline_page"),
        "asset_map_page": slice_obj.get("asset_map_page"),
    }
    if mode == "template":
        common["template_map_page"] = slice_obj.get("template_map_page")
    else:
        common["style_spec"] = slice_obj.get("style_spec")
    return common


def _build_prompt(prompt_template: str, slice_obj: dict, mode: str, extra: str = "") -> str:
    parts = [prompt_template.rstrip()]
    parts.append(
        "## 本页数据（JSON）\n```json\n"
        + json.dumps(_page_data(slice_obj, mode), ensure_ascii=False, indent=2)
        + "\n```"
    )
    if mode == "template":
        tpl_path = slice_obj.get("template_html_path")
        tpl_html = ""
        if tpl_path and Path(tpl_path).is_file():
            tpl_html = Path(tpl_path).read_text(encoding="utf-8")
        parts.append(
            "## 参考模板 HTML（布局/配色/字号/分区/装饰的依据——照它生成本页，按指令做文字/槽位/fallback.png 填充）\n"
            "```html\n" + tpl_html + "\n```"
        )
    if extra:
        parts.append(extra)
    parts.append("只返回完整 HTML 本体，不要任何解释、不要 markdown 代码块包裹。")
    return "\n\n".join(parts)


def _lint(mode: str, deck_dir: str, page_number: int, output_html: str, template_html: str | None):
    if mode == "template":
        cmd = [
            sys.executable, str(SCRIPT_DIR / "lint_page_html.py"),
            "--output", output_html, "--template", template_html or "",
        ]
    else:
        cmd = [
            sys.executable, str(SCRIPT_DIR / "lint_pages.py"),
            "--deck", deck_dir, "--page", str(page_number), "--json",
        ]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    reasons: list[str] = []
    if proc.returncode != 0:
        try:
            j = json.loads((proc.stdout or "").strip().splitlines()[-1])
            reasons = j.get("reasons", []) or [j.get("reason", "")]
        except Exception:  # noqa: BLE001
            reasons = [(proc.stdout or proc.stderr or "lint failed").strip()[:200]]
    return proc.returncode == 0, reasons


def _generate_once(prompt: str, page_id: str) -> str:
    result = call_tool(
        "html_page_generate",
        {"prompt": prompt},
        tool_call_id=f"call_html_page_generate_{page_id}",
    )
    html = result.get("html") or ""
    if not html.strip():
        raise ToolCallError("generated html is empty")
    return _strip_fence(html)


def _process_page(slice_path: Path, mode: str, prompt_template: str, deck_dir: str) -> dict:
    detail = {"page": None, "output": None, "lint_ok": False, "retried": False, "reason": ""}
    try:
        slice_obj = json.loads(slice_path.read_text(encoding="utf-8"))
    except Exception as exc:  # noqa: BLE001
        detail["reason"] = f"slice_unreadable({type(exc).__name__})"
        return detail
    page_number = slice_obj.get("page_number")
    page_id = slice_obj.get("page_id") or f"page_{int(page_number):03d}"
    output_html = slice_obj.get("output_html_path")
    template_html = slice_obj.get("template_html_path")
    detail["page"] = page_number
    detail["output"] = output_html

    try:
        prompt = _build_prompt(prompt_template, slice_obj, mode)
        html = _generate_once(prompt, page_id)
        Path(output_html).parent.mkdir(parents=True, exist_ok=True)
        Path(output_html).write_text(html, encoding="utf-8")
    except Exception as exc:  # noqa: BLE001
        detail["reason"] = f"generate_fail({type(exc).__name__}: {exc})"[:200]
        return detail

    lint_ok, reasons = _lint(mode, deck_dir, page_number, output_html, template_html)
    if lint_ok:
        detail["lint_ok"] = True
        return detail

    # 一次严格重出
    detail["retried"] = True
    extra = (
        "## 上一版自检未通过，必须修正以下问题后重出（其余保持不变）\n- "
        + "\n- ".join(str(r) for r in reasons[:20])
    )
    try:
        prompt2 = _build_prompt(prompt_template, slice_obj, mode, extra=extra)
        html2 = _generate_once(prompt2, page_id)
        Path(output_html).write_text(html2, encoding="utf-8")
    except Exception as exc:  # noqa: BLE001
        detail["reason"] = f"retry_generate_fail({type(exc).__name__})"[:200]
        return detail

    lint_ok2, reasons2 = _lint(mode, deck_dir, page_number, output_html, template_html)
    detail["lint_ok"] = lint_ok2
    if not lint_ok2:
        detail["reason"] = ("lint_fail: " + "; ".join(str(r) for r in reasons2[:10]))[:200]
    return detail


def run(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--deck", required=True)
    parser.add_argument("--mode", required=True, choices=["no_template", "template"])
    parser.add_argument("--prompt", required=True, help="prompt 模板 md 绝对路径")
    parser.add_argument("--concurrency", type=int, default=DEFAULT_CONCURRENCY)
    parser.add_argument("--pages", default=None,
                        help="只重生成指定页码，逗号分隔如 '2,3'；省略=全做。用于 review 后定向补做坏掉的页。")
    args = parser.parse_args(argv)

    deck_dir = Path(args.deck)
    htmls_dir = deck_dir / "htmls"
    if not htmls_dir.is_dir():
        print(json.dumps({"status": "fail", "reason": f"htmls dir not found: {htmls_dir}"}))
        return 1
    prompt_path = Path(args.prompt)
    if not prompt_path.is_file():
        print(json.dumps({"status": "fail", "reason": f"prompt not found: {prompt_path}"}))
        return 1
    prompt_template = prompt_path.read_text(encoding="utf-8")

    slice_paths = sorted(htmls_dir.glob("page_*.input.json"))
    if not slice_paths:
        print(json.dumps({"status": "fail", "reason": "no page_*.input.json slices found"}))
        return 1

    if args.pages:
        try:
            want = {int(x) for x in str(args.pages).replace("，", ",").split(",") if x.strip()}
        except ValueError:
            print(json.dumps({"status": "fail", "reason": f"--pages 解析失败: {args.pages!r}"}))
            return 1

        def _page_num(p: Path) -> int | None:
            digits = "".join(ch for ch in p.name.split(".")[0] if ch.isdigit())
            return int(digits) if digits else None

        slice_paths = [p for p in slice_paths if _page_num(p) in want]
        if not slice_paths:
            print(json.dumps({"status": "fail", "reason": f"--pages {sorted(want)} 没匹配到任何切片"}))
            return 1

    with ThreadPoolExecutor(max_workers=max(1, args.concurrency)) as pool:
        details = list(
            pool.map(
                lambda p: _process_page(p, args.mode, prompt_template, str(deck_dir)),
                slice_paths,
            )
        )

    details.sort(key=lambda d: (d.get("page") is None, d.get("page") or 0))
    ok_pages = [d["page"] for d in details if d["lint_ok"]]
    failed_pages = [d["page"] for d in details if not d["lint_ok"]]
    print(
        json.dumps(
            {
                "status": "ok",
                "page_count": len(details),
                "ok_pages": ok_pages,
                "failed_pages": failed_pages,
                "details": details,
            },
            ensure_ascii=False,
        )
    )
    return 0


if __name__ == "__main__":
    from _stage_status import cli_value, run_main
    sys.exit(run_main(run, cli_value("--deck"), "generating_pages", "generate_html"))
