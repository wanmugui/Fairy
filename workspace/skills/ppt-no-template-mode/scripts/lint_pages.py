#!/usr/bin/env python3
"""静态校验无模板模式 deck 产物。

对 `<deck>/htmls/page_xxx.html` 做纯静态检查，不调 LLM、不跑沙盒、不依赖任何外部服务。
覆盖 stage 5 自检和 stage 6 code-only 部分规则：

- ASSET_NOT_RENDERED   asset_map.image_assets 中存在 local_path 非空、但 HTML 里没有任何 <img src> 引用（CSS background-image: url('../assets/...') 不算渲染）
- MISSING_PICTURE_WRAPPER  HTML 中引用 ../assets/<id>.<ext> 的 <img> 缺少直接父级 <div id="picture-NNN">（id 等于对应 asset_map.image_assets[].slot_id）
- MISSING_PICTURE_SIZE  <div id="picture-NNN"> 缺少含 width/height 的内联 style（标准 style="width:100%;height:100%;"；下游靠它提取图片，缺了会丢图）
- MISSING_PICTURE_CLASS <div id="picture-NNN"> 缺少 class="picture"（下游按该 class 选取图片单元）
- ID_ON_IMG            <img> 标签自身带 id 属性（id 只能挂在外层包裹 div 上）
- BG_ASSET_URL_USED    出现 background-image: url('../assets/...') 写法——真实图片资产只能走 <img> 渲染单元
- HIDDEN_DISPLAY_ID    挂了 data-display-id 的元素被 display:none / visibility:hidden / opacity:0 屏蔽
- DUP_DISPLAY_ID       同页出现两处及以上相同的 data-display-id
- UNKNOWN_DISPLAY_ID   HTML 中 data-display-id 的值不在合法词表里（词表 = 本页 content 的 sub_point key + "title"；旧版 outline 带 display_items 数组时按旧清单对账）
- MISSING_DISPLAY_ITEM outline content 里有可见内容的 sub_point key 没有任何 data-display-id 兑现（"title" 合法但不强制——封面类页标题常由 sub_point 承载）
- MISSING_MOTIF_KEY    style_spec.page_type_variants[当前variant] 的 background/foreground motif_keys 未在 HTML 中以 data-motif-key 出现
- DEGRADED_PICTURE_SLOT id 形如 picture-* 或 class 含 picture 的块级元素，子树里出现了 data-display-id 或 scene-empty/image-missing/picture-placeholder 这类"文字冒充图片"的退化写法
- EMPTY_DECOR_CONTAINER CSS 里排版出来的 .card/.chip/.panel/.signal-card/.feature-grid 内部没有任何文字或图像节点
- OVERSIZED_DECORATIVE_BOX #ct 内空框（无文字、无 img/canvas/svg/table）尺寸 ≥120x120 且未挂 data-layer="fg-motif"+data-motif-key（best-effort：CSS resolution 只解析 inline style 和单层 class 尺寸）
- PAGE_SIZE_MISMATCH   外层 wrapper / #bg / #ct 的尺寸不是当前画布（默认 1280x720，--canvas 1600x900 可切）
- DUP_TEXT_PHRASE      同页可见正文里出现两处及以上相同的 ≥8 字中文短语（或 ≥12 字符英文）
- ORPHAN_VISIBLE_TEXT  body 内有可见文字节点含 ≥4 个中文字符（或 ≥8 个英文字符），但祖先链上没有任何元素挂 data-display-id（outline 外的"凭空"文字）
- BG_DECL_OUTSIDE_BG   background / background-color / background-image 声明出现在 <body> / .wrapper / .page / .slide / .container / #ct / #footer 等 #bg 之外的容器上（inline style 或 <style> 选择器）
- ORPHAN_LINE          标题类元素按 font-size + max-width 估算后末行只剩孤字/孤词
- GRADIENT_TEXT_CLIP   出现 background-clip:text（含 -webkit- 前缀）渐变文字——静态解析器读不到这种文字会丢字，须改实色 color
- BANNED_VISUAL_PROP   CSS 出现解析器不支持的视觉属性：clip-path / mask / filter / backdrop-filter / mix-blend-mode / 3D 变换或旋转（rotate/skew/matrix/perspective/*3d）
- BANNED_LAYOUT_FEATURE CSS 出现禁用的布局特性：calc() / 视口单位(vw/vh/vmin/vmax) / 容器查询(@container/cqw…) / CSS 动画(@keyframes/animation)
- BARE_TEXT_WITH_DECOR  「装饰空元素 + 裸文本」结构（如 <div><span class="dot"></span>裸文本</div>）——文字须用 <span>/<div> 包裹后再与装饰同级
- BANNED_TABLE_FEATURE  表格使用 rowspan / colspan / 嵌套 table（解析器只支持等列数的标准表格）

注：以上 CSS 类规则只扫 inline style 与 <style>，**不扫 <script>**，因此 ECharts 的 option 字符串不会被误判（图表仍走 ECharts，由下游单独处理）。

使用：
    uv run python skills/ppt-no-template-mode/scripts/lint_pages.py --deck <deck_dir> \
        [--page 5]                       # 只校单页
        [--only HIDDEN_DISPLAY_ID,...]   # 只跑指定规则
        [--report <path.json>]           # 把人文报告 / JSON 落到文件
        [--quiet]                         # 只打印 issue 数，不打印逐条明细
        [--json]                          # stdout 输出一行 JSON 摘要（subagent 自检模式）

退出码：
    0   无 issue
    1   存在 issue
    2   输入错误 / 文件缺失
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Iterable

try:
    import load_env
    load_env.load()
except (ImportError, AttributeError):
    pass

try:
    from bs4 import BeautifulSoup, NavigableString, Tag
except ModuleNotFoundError:  # pragma: no cover
    print(
        "[lint_pages] missing dependency `beautifulsoup4`; run `uv add beautifulsoup4`",
        file=sys.stderr,
    )
    sys.exit(2)


ALL_RULES = (
    "ASSET_NOT_RENDERED",
    "MISSING_PICTURE_WRAPPER",
    "MISSING_PICTURE_SIZE",
    "MISSING_PICTURE_CLASS",
    "ID_ON_IMG",
    "BG_ASSET_URL_USED",
    "HIDDEN_DISPLAY_ID",
    "DUP_DISPLAY_ID",
    "UNKNOWN_DISPLAY_ID",
    "MISSING_DISPLAY_ITEM",
    "MISSING_MOTIF_KEY",
    "DEGRADED_PICTURE_SLOT",
    "EMPTY_DECOR_CONTAINER",
    "OVERSIZED_DECORATIVE_BOX",
    "PAGE_SIZE_MISMATCH",
    "DUP_TEXT_PHRASE",
    "ORPHAN_VISIBLE_TEXT",
    "BG_DECL_OUTSIDE_BG",
    "ORPHAN_LINE",
    "GRADIENT_TEXT_CLIP",
    "BANNED_VISUAL_PROP",
    "BANNED_LAYOUT_FEATURE",
    "BARE_TEXT_WITH_DECOR",
    "BANNED_TABLE_FEATURE",
)

DEGRADED_PICTURE_CLASS_HITS = (
    "scene-empty",
    "image-missing",
    "picture-placeholder",
    "image-caption-as-image",
)

BG_OUTER_CLASS_HITS = ("wrapper", "page", "slide", "container")

BG_DECL_RE = re.compile(r"\bbackground(?:-color|-image)?\s*:", re.IGNORECASE)
BG_STYLE_OUTER_SELECTOR_RE = re.compile(
    r"(?:^|[\s>+~])(?:body|\.wrapper|\.page|\.slide|\.container|#ct|#footer)"
    r"\s*(?:::[a-z-]+|:[a-z-]+)?\s*$",
    re.IGNORECASE,
)

DECOR_BOX_MIN_PX = 120.0

DECOR_CARD_CLASS_PREFIXES = (
    "card",
    "chip",
    "panel",
    "signal-card",
    "signal-item",
    "feature-grid",
    "stat-row",
    "kpi-strip",
    "value-card-wrap",
    "trend-list",
)

HIDDEN_STYLE_PATTERNS = (
    re.compile(r"display\s*:\s*none", re.IGNORECASE),
    re.compile(r"visibility\s*:\s*hidden", re.IGNORECASE),
    re.compile(r"opacity\s*:\s*0(?:\.0+)?\s*(?:;|$)", re.IGNORECASE),
)

CHINESE_CHAR_RE = re.compile(r"[一-鿿]")

# --- static-parser banned CSS (scanned only over inline style + <style>, never <script>) ---
GRADIENT_TEXT_CLIP_RE = re.compile(
    r"(?:-webkit-)?background-clip\s*:\s*text", re.IGNORECASE
)
BANNED_VISUAL_PROP_RE = re.compile(
    r"\b(?:clip-path|(?:-webkit-)?mask(?:-image)?|filter|backdrop-filter|mix-blend-mode)\s*:",
    re.IGNORECASE,
)
BANNED_TRANSFORM_RE = re.compile(
    r"transform\s*:[^;{}]*\b(?:rotate(?:3d|x|y|z)?|skew[xy]?|matrix3?d?|perspective|translate3d|scale3d)\s*\(",
    re.IGNORECASE,
)
BANNED_LAYOUT_RE = re.compile(
    r"\bcalc\s*\("
    r"|(?<![\w.])-?\d*\.?\d+(?:vw|vh|vmin|vmax|cqw|cqh|cqi|cqb)\b"
    r"|@container\b|@keyframes\b|\banimation(?:-name|-duration)?\s*:",
    re.IGNORECASE,
)
DECOR_INLINE_TAGS = ("span", "i", "em", "b", "small")


@dataclass
class Issue:
    code: str
    detail: str
    target: str = ""
    severity: str = "error"  # error | warning

    def to_line(self) -> str:
        tgt = f" (target={self.target})" if self.target else ""
        return f"- [{self.code}] {self.detail}{tgt}"


@dataclass
class PageReport:
    page_tag: str
    html_path: str
    issues: list[Issue] = field(default_factory=list)

    def add(self, issue: Issue) -> None:
        self.issues.append(issue)

    @property
    def ok(self) -> bool:
        return not self.issues


# ------------------------------- rule helpers -------------------------------


def _inline_style(tag: Tag) -> str:
    return (tag.get("style") or "").strip()


def _text_of(tag: Tag) -> str:
    return tag.get_text(separator="", strip=True)


def _any_hidden_inline_style(style: str) -> str | None:
    for pat in HIDDEN_STYLE_PATTERNS:
        if pat.search(style):
            return pat.pattern
    return None


def _iter_classes(tag: Tag) -> list[str]:
    cls = tag.get("class")
    if not cls:
        return []
    return list(cls)


def _container_is_decor_card(tag: Tag) -> bool:
    classes = _iter_classes(tag)
    for c in classes:
        for prefix in DECOR_CARD_CLASS_PREFIXES:
            if c == prefix or c.startswith(prefix + "-"):
                return True
    return False


def _extract_sizes_from_css(css: str) -> dict[str, list[tuple[str, str]]]:
    """
    从 CSS 文本抽取形如 `.wrapper { width:1280px; height:720px }` / `html,body { ... }`
    的规则块里 width/height 声明。返回 {selector: [(prop, value), ...]}。
    """
    results: dict[str, list[tuple[str, str]]] = {}
    css = re.sub(r"/\*.*?\*/", "", css, flags=re.S)
    for selector, body in re.findall(r"([^{}]+)\{([^{}]*)\}", css):
        sel = selector.strip()
        if not sel:
            continue
        props: list[tuple[str, str]] = []
        for decl in body.split(";"):
            decl = decl.strip()
            if ":" not in decl:
                continue
            p, v = decl.split(":", 1)
            p = p.strip().lower()
            v = v.strip().lower()
            if p in {"width", "height", "max-width", "max-height", "aspect-ratio"}:
                props.append((p, v))
        if props:
            results[sel] = props
    return results


def _selector_targets_wrapper(selector: str) -> bool:
    sel_lc = selector.lower()
    # allow grouped selectors: "html,body" etc.
    parts = [s.strip() for s in sel_lc.split(",")]
    for part in parts:
        if part in {
            "html",
            "body",
            ".wrapper",
            ".slide",
            ".page",
            ".container",
            "#bg",
            "#ct",
        }:
            return True
    return False


CANVAS_W = "1280"   # 画布宽，--canvas 可覆盖（默认 1280x720 前端契约；对比实验用 1600x900）
CANVAS_H = "720"


def _dims_are_1280x720(
    sizes: dict[str, list[tuple[str, str]]],
) -> tuple[bool, list[str]]:
    """校验外层尺寸 == 当前画布（CANVAS_W×CANVAS_H，默认 1280×720，--canvas 可切）。

    接受两种合规写法：
      A. 显式 width:{W}px + height:{H}px （可能在 html/body/.wrapper/#bg/#ct）
      B. max-width:{W}px + max-height:{H}px + aspect-ratio:16/9（某一选择器集中声明）
    返回 (is_ok, evidence_selectors)。
    """
    evidence: list[str] = []
    has_fixed_w = False
    has_fixed_h = False
    has_max_w = False
    has_max_h = False
    has_aspect = False

    for sel, props in sizes.items():
        if not _selector_targets_wrapper(sel):
            continue
        for p, v in props:
            if p == "width" and (f"{CANVAS_W}px" in v or v == CANVAS_W):
                has_fixed_w = True
                evidence.append(f"{sel}:width={v}")
            elif p == "height" and (f"{CANVAS_H}px" in v or v == CANVAS_H):
                has_fixed_h = True
                evidence.append(f"{sel}:height={v}")
            elif p == "max-width" and f"{CANVAS_W}px" in v:
                has_max_w = True
                evidence.append(f"{sel}:max-width={v}")
            elif p == "max-height" and f"{CANVAS_H}px" in v:
                has_max_h = True
                evidence.append(f"{sel}:max-height={v}")
            elif p == "aspect-ratio" and ("16" in v and "9" in v):
                has_aspect = True
                evidence.append(f"{sel}:aspect-ratio={v}")
    ok = (has_fixed_w and has_fixed_h) or (has_max_w and has_max_h and has_aspect)
    return ok, evidence


def _has_image_like_descendant(tag: Tag) -> bool:
    if tag.find("img") is not None:
        return True
    # CSS background-image via inline style
    style = _inline_style(tag)
    if "background-image" in style.lower() or "background:" in style.lower():
        if "url(" in style.lower():
            return True
    for child in tag.find_all(True):
        cstyle = _inline_style(child)
        if "url(" in cstyle.lower() and "background" in cstyle.lower():
            return True
    return False


def _visible_text_of_page(soup: BeautifulSoup) -> str:
    # drop <script>/<style>/<title>/<meta> so DUP_TEXT_PHRASE does not
    # compare <title> 里的标题与 body <h1> 里的标题
    clone = BeautifulSoup(str(soup), "html.parser")
    for t in clone.find_all(["script", "style", "title", "meta", "link"]):
        t.decompose()
    # head 里其它元素也跳掉
    head = clone.find("head")
    if head:
        head.decompose()
    return clone.get_text(" ", strip=True)


_PHRASE_SPLIT_RE = re.compile(
    r"[\s，。；：？！、,\.;:\?!/·\|\-—…·→…\(\)\[\]\{\}<>\"“”‘’'`～~]+"
)


def _chinese_phrases(text: str, min_chars: int) -> list[str]:
    """把可见文字按常见分隔符切段，再筛出连续 ≥min_chars 中文字符的段。"""
    out: list[str] = []
    for seg in _PHRASE_SPLIT_RE.split(text):
        if not seg:
            continue
        # 只保留纯中文连续块；英数混排单独由英文规则处理
        run: list[str] = []
        for ch in seg:
            if CHINESE_CHAR_RE.match(ch):
                run.append(ch)
            else:
                if len(run) >= min_chars:
                    out.append("".join(run))
                run = []
        if len(run) >= min_chars:
            out.append("".join(run))
    return out


def _extract_font_size_px(style: str) -> float | None:
    m = re.search(r"font-size\s*:\s*([0-9]+(?:\.[0-9]+)?)px", style, re.IGNORECASE)
    if not m:
        return None
    return float(m.group(1))


def _extract_max_width_px(style: str) -> float | None:
    m = re.search(r"max-width\s*:\s*([0-9]+(?:\.[0-9]+)?)px", style, re.IGNORECASE)
    if not m:
        return None
    return float(m.group(1))


def _px_from_style(style: str, prop: str) -> float | None:
    m = re.search(
        rf"(?:^|;)\s*{re.escape(prop)}\s*:\s*([0-9]+(?:\.[0-9]+)?)px",
        style,
        re.IGNORECASE,
    )
    return float(m.group(1)) if m else None


def _extract_class_pixel_sizes(
    css: str,
) -> dict[str, tuple[float | None, float | None]]:
    """Parse simple `.class { width:Npx; height:Mpx }` rules from CSS text.

    Only handles single-class selectors (`.foo`); descendant/compound selectors
    are skipped. Used by the OVERSIZED_DECORATIVE_BOX rule as best-effort CSS
    resolution; nothing here matches a real CSS engine."""
    css = re.sub(r"/\*.*?\*/", "", css, flags=re.S)
    out: dict[str, tuple[float | None, float | None]] = {}
    for selector, body in re.findall(r"([^{}]+)\{([^{}]*)\}", css):
        m = re.match(r"^\.([a-zA-Z_][\w-]*)$", selector.strip())
        if not m:
            continue
        cname = m.group(1)
        w = h = None
        for decl in body.split(";"):
            decl = decl.strip()
            if ":" not in decl:
                continue
            p, v = decl.split(":", 1)
            p = p.strip().lower()
            v = v.strip()
            mm = re.match(r"([0-9.]+)px", v)
            if not mm:
                continue
            val = float(mm.group(1))
            if p == "width":
                w = val
            elif p == "height":
                h = val
        if w is not None or h is not None:
            out[cname] = (w, h)
    return out


def _resolve_pixel_size(
    tag: Tag, class_sizes: dict[str, tuple[float | None, float | None]]
) -> tuple[float | None, float | None]:
    """Best-effort: inline style first, then single-class CSS rules."""
    style = _inline_style(tag)
    w = _px_from_style(style, "width")
    h = _px_from_style(style, "height")
    if w is not None and h is not None:
        return w, h
    for c in _iter_classes(tag):
        cw, ch = class_sizes.get(c, (None, None))
        if w is None and cw is not None:
            w = cw
        if h is None and ch is not None:
            h = ch
        if w is not None and h is not None:
            break
    return w, h


def _has_bg_ancestor(tag: Tag) -> bool:
    parent = tag.parent
    while parent is not None:
        if hasattr(parent, "get") and parent.get("id") == "bg":
            return True
        parent = parent.parent
    return False


def _has_display_id_ancestor(tag: Tag | None) -> bool:
    cursor = tag
    while cursor is not None:
        if hasattr(cursor, "has_attr") and cursor.has_attr("data-display-id"):
            return True
        cursor = cursor.parent
    return False


# --------------------------------- rules ------------------------------------


def rule_hidden_display_id(soup: BeautifulSoup, report: PageReport) -> None:
    for tag in soup.select("[data-display-id]"):
        style = _inline_style(tag)
        hit = _any_hidden_inline_style(style)
        if hit:
            report.add(
                Issue(
                    "HIDDEN_DISPLAY_ID",
                    f"display_id={tag.get('data-display-id')} 带屏蔽样式 ({hit})",
                    target=f"{tag.name}#{tag.get('id') or ''}",
                )
            )


def rule_dup_display_id(soup: BeautifulSoup, report: PageReport) -> None:
    seen: dict[str, int] = {}
    for tag in soup.select("[data-display-id]"):
        did = tag.get("data-display-id") or ""
        seen[did] = seen.get(did, 0) + 1
    for did, count in seen.items():
        if count > 1:
            report.add(
                Issue(
                    "DUP_DISPLAY_ID",
                    f"display_id={did} 在页面中出现 {count} 次",
                    target=did,
                )
            )


def _iter_content_items(content: object) -> Iterable[tuple[str, dict]]:
    """Yield (key, sub_point) over an outline page's `content`, tolerating both
    schemas: current dict `{sub_point1: {...}}` and the older list
    `[{slot_id: "card_1", ...}, ...]`. For list items the key falls back to
    `slot_id` (the stable identifier in the old schema), then `sub_pointN`."""
    if isinstance(content, dict):
        for key, sp in content.items():
            yield str(key), sp
    elif isinstance(content, list):
        for i, sp in enumerate(content, start=1):
            if not isinstance(sp, dict):
                continue
            key = str(sp.get("slot_id") or f"sub_point{i}")
            yield key, sp


def rule_unknown_and_missing_display_item(
    soup: BeautifulSoup, outline_page: dict | None, report: PageReport
) -> None:
    if not outline_page:
        return
    # display_id 词表直接来自 content 的 sub_point key（不再有独立 display_items 数组）：
    #   allowed  = {"title"} ∪ content keys —— "title" 合法但不强制（封面类页标题常由 sub_point 承载）
    #   required = 有可见内容的 content keys —— 每个都必须有 data-display-id 兑现
    # 旧版 outline 带 display_items 数组时按旧清单对账（回放兼容）。
    allowed: set[str] = set()
    required: set[str] = set()
    legacy_items = outline_page.get("display_items") or []
    if legacy_items:
        for item in legacy_items:
            did = item.get("display_id")
            if did:
                allowed.add(did)
                required.add(did)
    else:
        allowed.add("title")
        for key, sp in _iter_content_items(outline_page.get("content")):
            if not isinstance(sp, dict):
                continue
            has_visible = any(
                str(sp.get(f) or "").strip()
                for f in ("sub_point_name", "text", "table", "chart", "picture")
            ) or bool(sp.get("sub_sub_points"))
            if has_visible:
                allowed.add(key)
                required.add(key)

    rendered: set[str] = set()
    for tag in soup.select("[data-display-id]"):
        did = tag.get("data-display-id")
        if did:
            rendered.add(did)

    for did in sorted(rendered - allowed):
        report.add(Issue("UNKNOWN_DISPLAY_ID", f"页面出现未声明的 display_id={did}", target=did))
    for did in sorted(required - rendered):
        report.add(
            Issue("MISSING_DISPLAY_ITEM", f"outline 声明的 display_id={did} 未兑现", target=did)
        )


def rule_empty_decor_container(soup: BeautifulSoup, report: PageReport) -> None:
    for tag in soup.find_all(True):
        if not isinstance(tag, Tag):
            continue
        if not _container_is_decor_card(tag):
            continue
        text = _text_of(tag)
        if text:
            continue
        if _has_image_like_descendant(tag):
            continue
        # 还允许 ECharts / table 这类本身就不靠 innerText 的
        if tag.find(["canvas", "svg", "table"]):
            continue
        # 如果容器里只有同样会被判"空装饰"的子容器，也视为空
        classes = ",".join(_iter_classes(tag)) or "(no-class)"
        report.add(
            Issue(
                "EMPTY_DECOR_CONTAINER",
                f"装饰容器 class=[{classes}] 内无文字/图像/图表内容",
                target=f"{tag.name}.{classes}",
            )
        )


def rule_page_size_mismatch(soup: BeautifulSoup, report: PageReport) -> None:
    css_blocks = [t.get_text() or "" for t in soup.find_all("style")]
    css = "\n".join(css_blocks)
    sizes = _extract_sizes_from_css(css)
    ok, evidence = _dims_are_1280x720(sizes)
    if ok:
        return
    # 再看内联 style：.wrapper / .page / .slide / .container / #bg / #ct / body
    inline_sizes: dict[str, list[tuple[str, str]]] = {}
    for sel, css_selector in (
        (".wrapper", ".wrapper"),
        (".page", ".page"),
        (".slide", ".slide"),
        (".container", ".container"),
        ("#bg", "#bg"),
        ("#ct", "#ct"),
        ("body", "body"),
    ):
        node = soup.select_one(css_selector)
        if not node:
            continue
        st = _inline_style(node)
        if not st:
            continue
        m_w = re.search(r"\bwidth\s*:\s*([0-9.]+)px", st, re.IGNORECASE)
        m_h = re.search(r"\bheight\s*:\s*([0-9.]+)px", st, re.IGNORECASE)
        m_mw = re.search(r"\bmax-width\s*:\s*([0-9.]+)px", st, re.IGNORECASE)
        m_mh = re.search(r"\bmax-height\s*:\s*([0-9.]+)px", st, re.IGNORECASE)
        m_ar = re.search(r"\baspect-ratio\s*:\s*([^;]+)", st, re.IGNORECASE)
        props = []
        if m_w:
            props.append(("width", m_w.group(1) + "px"))
        if m_h:
            props.append(("height", m_h.group(1) + "px"))
        if m_mw:
            props.append(("max-width", m_mw.group(1) + "px"))
        if m_mh:
            props.append(("max-height", m_mh.group(1) + "px"))
        if m_ar:
            props.append(("aspect-ratio", m_ar.group(1).strip().lower()))
        if props:
            inline_sizes[sel] = props
    if inline_sizes:
        sizes2 = {**sizes, **inline_sizes}
        ok2, evidence2 = _dims_are_1280x720(sizes2)
        if ok2:
            return
        evidence = evidence + evidence2
    report.add(
        Issue(
            "PAGE_SIZE_MISMATCH",
            f"未声明 1280x720 或等价尺寸；已知尺寸声明: {evidence or '无'}",
            target="wrapper/#bg/#ct",
        )
    )


_BLOCK_TAGS = (
    "div",
    "section",
    "article",
    "aside",
    "header",
    "footer",
    "main",
    "nav",
    "p",
    "h1",
    "h2",
    "h3",
    "h4",
    "h5",
    "h6",
    "li",
    "dt",
    "dd",
    "td",
    "th",
    "figcaption",
    "blockquote",
)


def _compressed_chinese(text: str) -> str:
    return "".join(CHINESE_CHAR_RE.findall(text or ""))


def _block_text_nodes(soup: BeautifulSoup) -> list[tuple[Tag, str]]:
    """收集块级元素的压缩中文文本，用于跨元素重复检测。跳过 head/script/style。"""
    body = soup.find("body") or soup
    out: list[tuple[Tag, str]] = []
    for tag in body.find_all(_BLOCK_TAGS):
        # 跳页码
        if tag.get("id") == "footer":
            continue
        if "footer" in (_iter_classes(tag) or []):
            continue
        raw = tag.get_text(" ", strip=True)
        compressed = _compressed_chinese(raw)
        if compressed:
            out.append((tag, compressed))
    return out


def _is_ancestor(a: Tag, b: Tag) -> bool:
    # a 是 b 的祖先？
    parent = b.parent
    while parent is not None:
        if parent is a:
            return True
        parent = parent.parent
    return False


def _target_desc(tag: Tag) -> str:
    return (
        f"{tag.name}"
        + (f"#{tag.get('id')}" if tag.get("id") else "")
        + (f".{'.'.join(_iter_classes(tag))}" if _iter_classes(tag) else "")
    )


def rule_dup_text_phrase(
    soup: BeautifulSoup,
    report: PageReport,
    *,
    min_chinese: int = 8,
    min_english: int = 16,
) -> None:
    # 中文：按块级元素抽压缩中文串；同一压缩串被 >=2 个不同 block 命中视为重复。
    pool: dict[str, list[Tag]] = {}
    for tag, compressed in _block_text_nodes(soup):
        if len(compressed) < min_chinese:
            continue
        pool.setdefault(compressed, []).append(tag)

    # 过滤祖先-后代冗余：若 targets 里 A 是 B 的祖先且它们压缩串相同，去掉 A。
    for phrase, tags in list(pool.items()):
        filtered: list[Tag] = []
        for t in tags:
            # 若 t 是别人祖先，跳过 t
            if any((t is not o) and _is_ancestor(t, o) for o in tags):
                continue
            filtered.append(t)
        pool[phrase] = filtered

    # 同一压缩串本身在 >=2 个非嵌套 block 中出现才算重复。
    for phrase, tags in sorted(pool.items(), key=lambda kv: (-len(kv[0]), kv[0])):
        if len(tags) < 2:
            continue
        report.add(
            Issue(
                "DUP_TEXT_PHRASE",
                f"压缩中文串「{phrase}」在 {len(tags)} 个块级元素中出现",
                target=",".join(_target_desc(t) for t in tags[:3]),
            )
        )

    # 英文：页面整体扫长句重复
    text = _visible_text_of_page(soup)
    en_tokens = re.findall(r"[A-Za-z][A-Za-z0-9 ,.'\-]{%d,}" % (min_english - 1), text)
    en_seen: dict[str, int] = {}
    for token in en_tokens:
        key = token.strip().lower()
        en_seen[key] = en_seen.get(key, 0) + 1
    for key, count in en_seen.items():
        if count < 2:
            continue
        report.add(
            Issue(
                "DUP_TEXT_PHRASE",
                f"英文正文 {count} 次出现「{key[:40]}...」",
                target=key[:40],
            )
        )


def _estimate_orphan(
    text: str, font_size_px: float, max_width_px: float
) -> tuple[bool, int, int]:
    if not text or font_size_px <= 0 or max_width_px <= 0:
        return False, 0, 0
    # 中文字符宽 ≈ font_size * 1.02；英文按 0.55 粗估
    cn = CHINESE_CHAR_RE.findall(text)
    cn_count = len(cn)
    non_cn = len(text) - cn_count
    # 平均每字符占用宽度
    avg_char_w = font_size_px * 1.02 if cn_count >= non_cn else font_size_px * 0.6
    chars_per_line = max(1, int(max_width_px / avg_char_w))
    total = len(text)
    if total <= chars_per_line:
        return False, total, chars_per_line
    last_line = total % chars_per_line
    if last_line == 0:
        return False, chars_per_line, chars_per_line
    ratio = last_line / chars_per_line
    return ratio < 0.2, last_line, chars_per_line


def rule_orphan_line(soup: BeautifulSoup, report: PageReport) -> None:
    # 抓所有 h1/h2 以及声明了 .title 的元素
    candidates: list[Tag] = []
    for tag in soup.find_all(["h1", "h2"]):
        candidates.append(tag)
    for tag in soup.select(".title"):
        if tag not in candidates:
            candidates.append(tag)
    for tag in candidates:
        text = _text_of(tag)
        if not text or len(text) < 10:
            continue
        style = _inline_style(tag)
        font_size = _extract_font_size_px(style)
        max_w = _extract_max_width_px(style)
        if font_size is None or max_w is None:
            # 尝试从 style 块读（按 #id / class 粗查）
            continue
        orphan, last_line, per_line = _estimate_orphan(text, font_size, max_w)
        if orphan:
            report.add(
                Issue(
                    "ORPHAN_LINE",
                    (
                        f"标题「{text[:24]}…」按 font-size={font_size}px, max-width={max_w}px"
                        f" 估算末行仅 {last_line}/{per_line} 字（<20%）"
                    ),
                    target=tag.name + "#" + (tag.get("id") or ""),
                )
            )


def rule_asset_not_rendered(
    soup: BeautifulSoup, asset_map_page: dict | None, report: PageReport
) -> None:
    """Item 2: every image_asset with a non-empty local_path must be referenced
    in the page HTML as <img src="../assets/<asset_id>.<ext>">. CSS
    background-image: url('../assets/...') does NOT count as rendering (forbidden
    for real image assets; tracked separately by BG_ASSET_URL_USED)."""
    if not asset_map_page:
        return
    img_refs = " ".join((img.get("src") or "") for img in soup.find_all("img"))
    for asset in asset_map_page.get("image_assets") or []:
        asset_id = asset.get("asset_id") or ""
        local = asset.get("local_path") or ""
        if not asset_id or not local:
            # stage 4 didn't deliver this asset; markdown says treat as no-image, skip
            continue
        if asset_id not in img_refs:
            report.add(
                Issue(
                    "ASSET_NOT_RENDERED",
                    f"asset_id={asset_id} 未在 HTML 中以 <img src> 渲染",
                    target=asset_id,
                )
            )


_ASSET_URL_BG_RE = re.compile(
    r"background(?:-image)?\s*:[^;]*url\(\s*['\"]?\.\./assets/[^)'\"]+['\"]?\s*\)",
    re.IGNORECASE,
)


def rule_picture_wrapper(soup: BeautifulSoup, report: PageReport) -> None:
    """Image render unit: every <img src="../assets/..."> must (a) have a direct
    parent <div id="picture-NNN"> and (b) NOT carry id itself. Also forbids
    real image assets being referenced via CSS background-image: url(...)."""
    pic_id_re = re.compile(r"^picture-\d{3}$")
    for img in soup.find_all("img"):
        src = img.get("src") or ""
        if not src.startswith("../assets/"):
            continue
        if img.has_attr("id"):
            report.add(
                Issue(
                    "ID_ON_IMG",
                    f"<img src={src}> 自身带 id={img.get('id')!r}；id 只能挂在外层包裹 div 上",
                    target=src,
                )
            )
        parent = img.parent
        parent_id = (parent.get("id") if isinstance(parent, Tag) else "") or ""
        if not (isinstance(parent, Tag) and parent.name == "div" and pic_id_re.match(parent_id)):
            report.add(
                Issue(
                    "MISSING_PICTURE_WRAPPER",
                    f"<img src={src}> 缺少直接父级 <div id='picture-NNN'>（实际父级=<{parent.name if isinstance(parent, Tag) else '?'} id={parent_id!r}>）",
                    target=src,
                )
            )
        else:
            style_val = (parent.get("style") or "").lower()
            if "width" not in style_val or "height" not in style_val:
                report.add(
                    Issue(
                        "MISSING_PICTURE_SIZE",
                        f"<div id={parent_id!r}> 缺少含 width/height 的内联 style"
                        f"（标准 style=\"width:100%;height:100%;\"）；下游靠该属性提取图片，缺了会整张丢图",
                        target=src,
                    )
                )
            if "picture" not in (parent.get("class") or []):
                report.add(
                    Issue(
                        "MISSING_PICTURE_CLASS",
                        f"<div id={parent_id!r}> 缺少 class=\"picture\"；下游按该 class 选取图片单元",
                        target=src,
                    )
                )

    for m in _ASSET_URL_BG_RE.finditer(str(soup)):
        snippet = m.group(0)[:120]
        report.add(
            Issue(
                "BG_ASSET_URL_USED",
                f"出现 CSS background-image 引用 ../assets/ 资产：{snippet}",
                target=snippet,
            )
        )


def rule_missing_motif_key(
    soup: BeautifulSoup,
    style_spec: dict | None,
    outline_page: dict | None,
    report: PageReport,
) -> None:
    """Item 4: motif_keys declared on this page's variant's background_motif_recipe
    / foreground_motif_recipe must each appear as data-motif-key in the HTML."""
    if not style_spec or not outline_page:
        return
    variant_id = outline_page.get("page_type_variant_id") or ""
    if not variant_id:
        return
    variant = None
    for v in style_spec.get("page_type_variants") or []:
        if v.get("variant_id") == variant_id:
            variant = v
            break
    if not variant:
        return
    required: list[str] = []
    for recipe_key in ("background_motif_recipe", "foreground_motif_recipe"):
        recipe = variant.get(recipe_key) or {}
        for k in recipe.get("motif_keys") or []:
            if isinstance(k, str) and k:
                required.append(k)
    rendered = {
        t.get("data-motif-key") for t in soup.select("[data-motif-key]")
    }
    for key in required:
        if key not in rendered:
            report.add(
                Issue(
                    "MISSING_MOTIF_KEY",
                    f"motif_key={key} 未在 HTML 中以 data-motif-key 出现 (variant={variant_id})",
                    target=key,
                )
            )


def rule_degraded_picture_slot(
    soup: BeautifulSoup, report: PageReport
) -> None:
    """Item 9: picture slots must hold actual images, not text-faking-image.
    Forbids data-display-id descendants and bad class names inside picture nodes."""
    pic_nodes: list[Tag] = []
    seen: set[int] = set()
    for tag in soup.select('[id^="picture-"]'):
        if id(tag) not in seen:
            pic_nodes.append(tag)
            seen.add(id(tag))
    for tag in soup.find_all(class_="picture"):
        if id(tag) not in seen:
            pic_nodes.append(tag)
            seen.add(id(tag))

    for pic in pic_nodes:
        for desc in pic.find_all(True):
            if desc.has_attr("data-display-id"):
                report.add(
                    Issue(
                        "DEGRADED_PICTURE_SLOT",
                        f"图位 {_target_desc(pic)} 子树含 data-display-id（文字冒充图片）",
                        target=_target_desc(pic),
                    )
                )
                break
        for desc in pic.find_all(True):
            classes = _iter_classes(desc)
            bad = next((c for c in classes if c in DEGRADED_PICTURE_CLASS_HITS), None)
            if bad:
                report.add(
                    Issue(
                        "DEGRADED_PICTURE_SLOT",
                        f"图位 {_target_desc(pic)} 子树含 class={bad}",
                        target=_target_desc(pic),
                    )
                )
                break


def rule_oversized_decorative_box(
    soup: BeautifulSoup, report: PageReport
) -> None:
    """Item 10 (best-effort): elements inside #ct that have no visible text/image
    content but a CSS-declared size of ≥120×120 must be a fg-motif. Only resolves
    inline style and single-class width/height; misses anything that depends on
    flex/grid sizing, percentages, or parent-relative dimensions."""
    ct = soup.find(id="ct")
    if not ct:
        return
    css_blocks = "\n".join((s.get_text() or "") for s in soup.find_all("style"))
    class_sizes = _extract_class_pixel_sizes(css_blocks)
    script_text = "\n".join((s.get_text() or "") for s in soup.find_all("script"))

    for tag in ct.find_all(True):
        if not isinstance(tag, Tag):
            continue
        # ECharts 挂载点：静态 HTML 里本来就是空的（JS 运行时填充），不是空装饰框
        tid = (tag.get("id") or "").strip()
        if tid.startswith("chart") and tid in script_text:
            continue
        # Has visible text -> not an empty box
        if tag.get_text(strip=True):
            continue
        # Has image-like descendant -> carries content
        if tag.find(["img", "canvas", "svg", "table", "picture"]):
            continue
        # Already a registered fg-motif -> allowed
        if (
            tag.get("data-layer") == "fg-motif"
            and (tag.get("data-motif-key") or "").strip()
        ):
            continue
        w, h = _resolve_pixel_size(tag, class_sizes)
        if w is None or h is None:
            continue
        if w >= DECOR_BOX_MIN_PX and h >= DECOR_BOX_MIN_PX:
            report.add(
                Issue(
                    "OVERSIZED_DECORATIVE_BOX",
                    f"#ct 内空框 {_target_desc(tag)} 约 {int(w)}x{int(h)}，未挂 fg-motif",
                    target=_target_desc(tag),
                )
            )


def _is_inside_motif(tag: Tag | None) -> bool:
    cur = tag
    while isinstance(cur, Tag):
        if (cur.get("data-layer") or "") in ("bg-motif", "fg-motif"):
            return True
        cur = cur.parent
    return False


def _is_inside_footer(tag: Tag | None) -> bool:
    cursor = tag
    while cursor is not None:
        if hasattr(cursor, "get"):
            if cursor.get("id") == "footer":
                return True
            if "footer" in (_iter_classes(cursor) or []):
                return True
        cursor = cursor.parent
    return False


def rule_orphan_visible_text(
    soup: BeautifulSoup, report: PageReport
) -> None:
    """Item 11: any visible text node in <body> with ≥4 chinese chars (or
    ≥8 latin letters) must have an ancestor carrying data-display-id.

    Footer (page-number) text is exempt: per stage-05 rules the footer renders
    `outline_page.page_number_label`, which is not part of display_items."""
    body = soup.find("body")
    if not body:
        return
    seen_targets: set[str] = set()
    for ns in body.find_all(string=True):
        if not isinstance(ns, NavigableString):
            continue
        parent = ns.parent
        if parent is None:
            continue
        # Skip non-rendered subtrees
        if parent.name in ("script", "style", "title", "meta", "link", "head"):
            continue
        # Footer carries page_number_label, not a display_item
        if _is_inside_footer(parent):
            continue
        # 骨架/装饰件（meta 条 / eyebrow / footbar 等 data-layer motif）内的短语是设计系统文字，
        # 不属于 outline 正文，不算孤立文字（重复滥用仍由 DUP_TEXT_PHRASE 与人审兜底）
        if _is_inside_motif(parent):
            continue
        text = str(ns).strip()
        if not text:
            continue
        cn = len(CHINESE_CHAR_RE.findall(text))
        en = sum(1 for c in text if c.isalpha() and ord(c) < 128)
        if cn < 4 and en < 8:
            continue
        if _has_display_id_ancestor(parent):
            continue
        # Suppress dup reports on the same closest carrier
        tgt = _target_desc(parent)
        if tgt in seen_targets:
            continue
        seen_targets.add(tgt)
        report.add(
            Issue(
                "ORPHAN_VISIBLE_TEXT",
                f"无 data-display-id 祖先的可见文字「{text[:24]}…」",
                target=tgt,
            )
        )


def rule_bg_decl_outside_bg(
    soup: BeautifulSoup, report: PageReport
) -> None:
    """Item 12: background / background-color / background-image declarations
    may only appear on #bg or its descendants. Catches inline style on
    body/.wrapper/.page/.slide/.container/#ct/#footer, and <style> selectors
    targeting those same containers."""
    # 1) <body> inline style
    body = soup.find("body")
    if body and BG_DECL_RE.search(_inline_style(body)):
        report.add(
            Issue("BG_DECL_OUTSIDE_BG", "<body> inline style 含 background 声明", target="body")
        )
    # 2) Inline style on outer containers (#ct, #footer, .wrapper/.page/.slide/.container)
    inline_seen = False
    for tag in soup.find_all(True):
        if not isinstance(tag, Tag) or tag is body:
            continue
        tid = tag.get("id") or ""
        if tid == "bg":
            continue
        if _has_bg_ancestor(tag):
            continue
        classes = _iter_classes(tag)
        is_outer = tid in ("ct", "footer") or any(
            c in BG_OUTER_CLASS_HITS for c in classes
        )
        if not is_outer:
            continue
        if BG_DECL_RE.search(_inline_style(tag)):
            report.add(
                Issue(
                    "BG_DECL_OUTSIDE_BG",
                    f"外层容器 {_target_desc(tag)} inline style 含 background 声明",
                    target=_target_desc(tag),
                )
            )
            inline_seen = True
            break  # one per page is enough to surface the issue
    # 3) <style> block selectors targeting outer containers
    if inline_seen:
        return
    for sty in soup.find_all("style"):
        css = sty.get_text() or ""
        css_no_comment = re.sub(r"/\*.*?\*/", "", css, flags=re.S)
        hit = False
        for selectors, body_decl in re.findall(
            r"([^{}]+)\{([^{}]*)\}", css_no_comment
        ):
            if not BG_DECL_RE.search(body_decl):
                continue
            for sel in selectors.split(","):
                s = sel.strip()
                if not s:
                    continue
                if BG_STYLE_OUTER_SELECTOR_RE.search(s) and "#bg" not in s:
                    report.add(
                        Issue(
                            "BG_DECL_OUTSIDE_BG",
                            f"<style> 选择器命中外层容器: {s[:80]}",
                            target=s[:80],
                        )
                    )
                    hit = True
                    break
            if hit:
                break
        if hit:
            break


def _iter_css_sources(soup: BeautifulSoup) -> Iterable[tuple[str, str]]:
    """Yield (target_desc, css_text) for every inline style attr and <style> block.

    <script> is deliberately never yielded, so ECharts `option` strings (which can
    legitimately contain `.filter(`, `animation`, etc. inside JS) are not scanned."""
    for tag in soup.find_all(style=True):
        if not isinstance(tag, Tag):
            continue
        st = tag.get("style") or ""
        if st.strip():
            yield _target_desc(tag), st
    for sty in soup.find_all("style"):
        css = sty.get_text() or ""
        if css.strip():
            yield "<style>", css


def rule_gradient_text_clip(soup: BeautifulSoup, report: PageReport) -> None:
    for target, css in _iter_css_sources(soup):
        if GRADIENT_TEXT_CLIP_RE.search(css):
            report.add(
                Issue(
                    "GRADIENT_TEXT_CLIP",
                    "出现 background-clip:text 渐变文字（静态解析器读不到会丢字）；改用实色 color",
                    target=target,
                )
            )
            return


def rule_banned_visual_prop(soup: BeautifulSoup, report: PageReport) -> None:
    for target, css in _iter_css_sources(soup):
        m = BANNED_VISUAL_PROP_RE.search(css) or BANNED_TRANSFORM_RE.search(css)
        if m:
            report.add(
                Issue(
                    "BANNED_VISUAL_PROP",
                    f"出现解析器不支持的视觉属性：{m.group(0).strip()[:60]}",
                    target=target,
                )
            )
            return


def rule_banned_layout_feature(soup: BeautifulSoup, report: PageReport) -> None:
    for target, css in _iter_css_sources(soup):
        m = BANNED_LAYOUT_RE.search(css)
        if m:
            report.add(
                Issue(
                    "BANNED_LAYOUT_FEATURE",
                    f"出现禁用的布局特性（calc/视口单位/容器查询/动画）：{m.group(0).strip()[:60]}",
                    target=target,
                )
            )
            return


def rule_bare_text_with_decor(soup: BeautifulSoup, report: PageReport) -> None:
    """§11: ban `<div><span class="dot"></span>裸文本</div>`. Flags an element that
    has BOTH a bare (non-whitespace) direct text child AND an empty decorative inline
    child (span/i/em/b/small with no text and no image). The compliant form wraps the
    text in its own <span>/<div>, so the text is no longer a bare direct child."""
    body = soup.find("body") or soup
    for tag in body.find_all(True):
        if not isinstance(tag, Tag) or tag.name in ("script", "style"):
            continue
        has_bare_text = False
        decor_child: Tag | None = None
        for child in tag.children:
            if isinstance(child, NavigableString):
                txt = str(child).strip()
                if txt and (CHINESE_CHAR_RE.search(txt) or any(c.isalpha() for c in txt)):
                    has_bare_text = True
            elif isinstance(child, Tag):
                if (
                    child.name in DECOR_INLINE_TAGS
                    and not child.get_text(strip=True)
                    and child.find(["img", "svg", "canvas", "picture"]) is None
                ):
                    decor_child = child
        if has_bare_text and decor_child is not None:
            report.add(
                Issue(
                    "BARE_TEXT_WITH_DECOR",
                    f"装饰空<{decor_child.name}>与裸文本同级（§11）；文字须用 <span>/<div> 包裹",
                    target=_target_desc(tag),
                )
            )


def rule_banned_table_feature(soup: BeautifulSoup, report: PageReport) -> None:
    for cell in soup.find_all(["td", "th"]):
        for attr in ("rowspan", "colspan"):
            if cell.has_attr(attr):
                report.add(
                    Issue(
                        "BANNED_TABLE_FEATURE",
                        f"<{cell.name}> 使用 {attr}（禁止合并单元格）",
                        target=_target_desc(cell),
                    )
                )
                return
    for tbl in soup.find_all("table"):
        if tbl.find("table") is not None:
            report.add(
                Issue(
                    "BANNED_TABLE_FEATURE",
                    "出现嵌套 table（禁止）",
                    target=_target_desc(tbl),
                )
            )
            return


# ---------------------------- orchestration ---------------------------------


def _load_outline_pages(deck: Path) -> dict[int, dict]:
    outline_path = deck / "outline.json"
    if not outline_path.is_file():
        return {}
    try:
        data = json.loads(outline_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {}
    pages = {}
    for page in data.get("pages") or []:
        pn = page.get("page_number")
        if isinstance(pn, int):
            pages[pn] = page
    return pages


def _load_asset_map_pages(deck: Path) -> dict[int, dict]:
    p = deck / "asset_map.json"
    if not p.is_file():
        return {}
    try:
        data = json.loads(p.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return {}
    pages: dict[int, dict] = {}
    for page in data.get("pages") or []:
        pn = page.get("page_number")
        if isinstance(pn, int):
            pages[pn] = page
    return pages


def _load_style_spec(deck: Path) -> dict | None:
    p = deck / "style_spec.json"
    if not p.is_file():
        return None
    try:
        return json.loads(p.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return None


def _page_number_from_filename(name: str) -> int | None:
    m = re.match(r"page_(\d{3,})\.html$", name)
    return int(m.group(1)) if m else None


def lint_deck(
    deck: Path,
    *,
    only_pages: Iterable[int] | None = None,
    only_rules: set[str] | None = None,
) -> list[PageReport]:
    htmls_dir = deck / "htmls"
    if not htmls_dir.is_dir():
        raise SystemExit(f"[lint_pages] htmls dir not found: {htmls_dir}")
    outline_pages = _load_outline_pages(deck)
    asset_map_pages = _load_asset_map_pages(deck)
    style_spec = _load_style_spec(deck)

    reports: list[PageReport] = []
    html_paths = sorted(htmls_dir.glob("page_*.html"))
    for html_path in html_paths:
        pn = _page_number_from_filename(html_path.name)
        if pn is None:
            continue
        if only_pages is not None and pn not in only_pages:
            continue
        html = html_path.read_text(encoding="utf-8", errors="replace")
        soup = BeautifulSoup(html, "html.parser")
        report = PageReport(page_tag=html_path.stem, html_path=str(html_path))
        outline_page = outline_pages.get(pn)
        asset_map_page = asset_map_pages.get(pn)

        def _enabled(code: str) -> bool:
            return only_rules is None or code in only_rules

        if _enabled("ASSET_NOT_RENDERED"):
            rule_asset_not_rendered(soup, asset_map_page, report)
        if (
            _enabled("MISSING_PICTURE_WRAPPER")
            or _enabled("MISSING_PICTURE_SIZE")
            or _enabled("MISSING_PICTURE_CLASS")
            or _enabled("ID_ON_IMG")
            or _enabled("BG_ASSET_URL_USED")
        ):
            rule_picture_wrapper(soup, report)
            if only_rules is not None:
                report.issues = [
                    i for i in report.issues if i.code in only_rules
                ]
        if _enabled("HIDDEN_DISPLAY_ID"):
            rule_hidden_display_id(soup, report)
        if _enabled("DUP_DISPLAY_ID"):
            rule_dup_display_id(soup, report)
        if _enabled("UNKNOWN_DISPLAY_ID") or _enabled("MISSING_DISPLAY_ITEM"):
            rule_unknown_and_missing_display_item(
                soup, outline_page, report
            )
            # 下面的过滤在 report 里做，保持 rule 生成
            if only_rules is not None:
                report.issues = [
                    i for i in report.issues if i.code in only_rules
                ]
        if _enabled("MISSING_MOTIF_KEY"):
            rule_missing_motif_key(soup, style_spec, outline_page, report)
        if _enabled("DEGRADED_PICTURE_SLOT"):
            rule_degraded_picture_slot(soup, report)
        if _enabled("EMPTY_DECOR_CONTAINER"):
            rule_empty_decor_container(soup, report)
        if _enabled("OVERSIZED_DECORATIVE_BOX"):
            rule_oversized_decorative_box(soup, report)
        if _enabled("PAGE_SIZE_MISMATCH"):
            rule_page_size_mismatch(soup, report)
        if _enabled("DUP_TEXT_PHRASE"):
            rule_dup_text_phrase(soup, report)
        if _enabled("ORPHAN_VISIBLE_TEXT"):
            rule_orphan_visible_text(soup, report)
        if _enabled("BG_DECL_OUTSIDE_BG"):
            rule_bg_decl_outside_bg(soup, report)
        if _enabled("ORPHAN_LINE"):
            rule_orphan_line(soup, report)
        if _enabled("GRADIENT_TEXT_CLIP"):
            rule_gradient_text_clip(soup, report)
        if _enabled("BANNED_VISUAL_PROP"):
            rule_banned_visual_prop(soup, report)
        if _enabled("BANNED_LAYOUT_FEATURE"):
            rule_banned_layout_feature(soup, report)
        if _enabled("BARE_TEXT_WITH_DECOR"):
            rule_bare_text_with_decor(soup, report)
        if _enabled("BANNED_TABLE_FEATURE"):
            rule_banned_table_feature(soup, report)

        reports.append(report)
    return reports


def _format_text_report(reports: list[PageReport], *, quiet: bool) -> str:
    lines: list[str] = []
    total_issues = sum(len(r.issues) for r in reports)
    lines.append(f"deck lint: pages={len(reports)} issues={total_issues}")
    for r in reports:
        tag = r.page_tag
        if r.ok:
            if not quiet:
                lines.append(f"  {tag}: OK")
            continue
        lines.append(f"  {tag}: {len(r.issues)} issue(s)")
        if quiet:
            continue
        for issue in r.issues:
            lines.append(f"    {issue.to_line()}")
    return "\n".join(lines)


def _build_json_report(reports: list[PageReport]) -> dict:
    return {
        "page_count": len(reports),
        "issue_count": sum(len(r.issues) for r in reports),
        "pages": [
            {
                "page_tag": r.page_tag,
                "html_path": r.html_path,
                "issues": [asdict(i) for i in r.issues],
            }
            for r in reports
        ],
    }


def _build_json_summary(reports: list[PageReport]) -> dict:
    """Compact one-line JSON used by stage-5 subagent self-check (--json mode)."""
    total = sum(len(r.issues) for r in reports)
    reasons: list[str] = []
    for r in reports:
        for i in r.issues:
            line = f"[{r.page_tag}] [{i.code}] {i.detail}"
            reasons.append(line[:200])
    return {
        "status": "ok" if total == 0 else "fail",
        "pages": [r.page_tag for r in reports],
        "issue_count": total,
        "reasons": reasons,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--deck", required=True, help="deck working directory")
    parser.add_argument(
        "--page",
        action="append",
        type=int,
        default=None,
        help="只校指定页（可重复传），默认全部",
    )
    parser.add_argument(
        "--only",
        default=None,
        help=f"只跑指定规则，逗号分隔。可选: {','.join(ALL_RULES)}",
    )
    parser.add_argument("--canvas", default="1280x720",
                        help="画布尺寸 WxH，默认 1280x720（前端契约）；对比实验可传 1600x900")
    parser.add_argument("--report", default=None, help="把 JSON 报告写到此路径")
    parser.add_argument("--quiet", action="store_true", help="不打印逐条明细")
    parser.add_argument(
        "--json",
        action="store_true",
        help="stdout 输出一行 JSON 摘要（subagent 自检模式，覆盖默认人文输出）",
    )
    args = parser.parse_args(argv)
    global CANVAS_W, CANVAS_H
    if "x" in (args.canvas or ""):
        w, h = args.canvas.lower().split("x", 1)
        CANVAS_W, CANVAS_H = w.strip(), h.strip()

    deck = Path(args.deck).resolve()
    if not deck.is_dir():
        print(f"[lint_pages] deck not found: {deck}", file=sys.stderr)
        return 2

    only_rules = None
    if args.only:
        only_rules = {r.strip().upper() for r in args.only.split(",") if r.strip()}
        unknown = only_rules - set(ALL_RULES)
        if unknown:
            print(
                f"[lint_pages] unknown rules: {sorted(unknown)}; valid: {ALL_RULES}",
                file=sys.stderr,
            )
            return 2

    reports = lint_deck(
        deck, only_pages=set(args.page) if args.page else None, only_rules=only_rules
    )

    if args.json:
        print(json.dumps(_build_json_summary(reports), ensure_ascii=False))
    else:
        print(_format_text_report(reports, quiet=args.quiet))

    if args.report:
        out = Path(args.report).resolve()
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(
            json.dumps(_build_json_report(reports), ensure_ascii=False, indent=2),
            encoding="utf-8",
        )

    total = sum(len(r.issues) for r in reports)
    return 1 if total > 0 else 0


if __name__ == "__main__":
    sys.exit(main())
