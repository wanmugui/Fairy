#!/usr/bin/env python3
"""ppt-template-mode stage 5 HTML output linter.

Runs structural / placement checks on a generated page HTML against the
reference template HTML it was based on. Catches the hard structural
errors that frequently slip through model-side self-check: basic file
integrity, busted `#bg` / `#ct` open tags, dropped fixed template imgs
(fallback.png excepted — it is a fillable image slot, see lint()),
unfilled fallback.png image slots,
new `<script>` tags absent from the template, duplicate `style=`,
background declarations escaping the `#bg` container, uncleared
template placeholder text (`内容内容内容` / `Lorem ipsum` / `第x页，共xx页` …),
static-parser-incompatible CSS (background-clip:text gradient text,
clip-path/mask/filter/backdrop-filter/mix-blend-mode, 3D-or-rotate transforms,
calc()/viewport-unit/container-query/animation), non-standard tables
(rowspan/colspan/nested table), and — for themed templates — dropped theme
stylesheet `<link>`s or hardcoded/redefined `var(--…)` theme colors (the theme
`<head>` links and theme-color variables must be preserved verbatim, never
hardcoded or overridden). The CSS checks strip `<script>` first, so ECharts
option strings are never flagged (charts remain a JS exception).

Usage:
    python lint_page_html.py --output <output_html_path> --template <template_html_path>

Exit codes:
    0  all checks passed
    2  any check failed

Output (one line of JSON on stdout):
    ok   -> {"status":"ok", "output":"...", "len":N}
    fail -> {"status":"fail", "output":"...", "len":N, "reasons":[...]}
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


BG_DECL_RE = re.compile(r"\bbackground(?:-color|-image)?\s*:", re.IGNORECASE)
OUTER_ATTR_RE = re.compile(
    r'\bid=["\'](?:ct|footer)["\']'
    r'|\bclass=["\'][^"\']*\b(?:wrapper|page|slide|container)\b[^"\']*["\']',
    re.IGNORECASE,
)
OUTER_SELECTOR_RE = re.compile(
    r"(?:^|[\s>+~])(?:body|\.wrapper|\.page|\.slide|\.container|#ct|#footer)"
    r"\s*(?:::[a-z-]+|:[a-z-]+)?\s*$",
    re.IGNORECASE,
)
PLACEHOLDER_PATTERNS = (
    r"(?:内容){3,}",
    r"(?:标题){3,}",
    r"(?:小标题){2,}",
    r"(?:流程描述){2,}",
    r"Lorem\s+ipsum",
    r"第\s*x\s*页.*?共\s*xx\s*页",
)

PICTURE_ID_RE = re.compile(r'^picture-\d{3}$')
IMG_TAG_RE = re.compile(r'<img\b[^>]*>', re.IGNORECASE)
IMG_ID_ATTR_RE = re.compile(r'\bid\s*=\s*["\'][^"\']*["\']', re.IGNORECASE)
IMG_SRC_ATTR_RE = re.compile(r'\bsrc\s*=\s*["\']([^"\']+)["\']', re.IGNORECASE)
ASSET_URL_BG_RE = re.compile(
    r"background(?:-image)?\s*:[^;]*url\(\s*['\"]?\.\./assets/[^)'\"]+['\"]?\s*\)",
    re.IGNORECASE,
)

# --- static-parser banned CSS (scanned after stripping <script>, so ECharts option
# strings are never matched; ECharts charts remain a sanctioned JS exception) ---
SCRIPT_BLOCK_RE = re.compile(r"<script\b[^>]*>.*?</script>", re.DOTALL | re.IGNORECASE)
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

# --- theme switching: theme stylesheet <link> + var(--…) theme-color variables ---
# Themed templates carry <link rel="stylesheet" href="../themes/light.css"> etc. in
# <head> and reference colors via var(--primary/--secondary/--background/--text/--border).
# Both must survive into the output: keep every theme <link> verbatim, keep referencing
# var(--…) (don't hardcode the theme colors), and never redefine/override those custom
# properties (they are defined ONLY in the external theme CSS).
LINK_TAG_RE = re.compile(r"<link\b[^>]*>", re.IGNORECASE)
HREF_ATTR_RE = re.compile(r'\bhref\s*=\s*["\']([^"\']+)["\']', re.IGNORECASE)
REL_STYLESHEET_RE = re.compile(
    r'\brel\s*=\s*["\'][^"\']*\bstylesheet\b[^"\']*["\']', re.IGNORECASE
)
VAR_USE_RE = re.compile(r"var\(\s*(--[a-zA-Z][\w-]*)")
CORE_THEME_VARS = ("--primary", "--secondary", "--background", "--text", "--border")


def _is_style_link(tag: str, href: str) -> bool:
    hl = href.lower()
    return bool(REL_STYLESHEET_RE.search(tag)) or hl.endswith(".css") or "/themes/" in hl


def check_theme_links(html: str, template_html: str, reasons: list[str]) -> None:
    """Every theme/style <link href> in the template must survive into the output
    verbatim. These files hold the theme-color variable values and are the basis of
    theme switching; dropping the <link> breaks the theme."""
    tpl_hrefs: list[str] = []
    for m in LINK_TAG_RE.finditer(template_html):
        tag = m.group(0)
        href_m = HREF_ATTR_RE.search(tag)
        if href_m and _is_style_link(tag, href_m.group(1)):
            tpl_hrefs.append(href_m.group(1))
    if not tpl_hrefs:
        return
    out_hrefs: set[str] = set()
    for m in LINK_TAG_RE.finditer(html):
        href_m = HREF_ATTR_RE.search(m.group(0))
        if href_m:
            out_hrefs.add(href_m.group(1))
    missing = [h for h in dict.fromkeys(tpl_hrefs) if h not in out_hrefs]
    if missing:
        reasons.append(
            "template theme stylesheet <link> dropped from output (must keep verbatim): "
            + ", ".join(missing)
        )


def check_theme_vars(html: str, template_html: str, reasons: list[str]) -> None:
    """Theme colors are referenced via var(--…) and defined only in the external theme
    CSS. The output must (1) keep referencing var(--…) rather than hardcoding every
    color, and (2) never redefine/override the theme custom properties. <script> is
    stripped first so ECharts option strings are never scanned."""
    tpl_vars = {m.group(1) for m in VAR_USE_RE.finditer(template_html)}
    if not tpl_vars:
        return
    out_css = SCRIPT_BLOCK_RE.sub(" ", html)
    out_vars = {m.group(1) for m in VAR_USE_RE.finditer(out_css)}
    if not out_vars:
        reasons.append(
            "template uses theme var(--…) colors but output has none "
            "(theme colors were hardcoded; keep var(--…) as in the template)"
        )
    # A custom-property definition `--name:` never appears inside a var(--name) usage
    # (there the name is followed by ')' or ','), so this only catches real overrides.
    guarded = set(tpl_vars) | set(CORE_THEME_VARS)
    for name in sorted(guarded):
        if re.search(r"(?<![\w-])" + re.escape(name) + r"\s*:", out_css):
            reasons.append(
                f"theme var {name} is redefined/overridden in output "
                f"(reference it via var({name}); it is defined only in the external "
                f"theme CSS and must never be redefined here)"
            )
            break


def check_picture_wrapper(html: str, reasons: list[str]) -> None:
    """Each <img src="../assets/<id>.<ext>"> must have a direct parent
    <div id="picture-NNN"> wrapper. <img> itself must not carry id. Real
    image assets must NOT be referenced via CSS background-image: url(...)."""
    # 1) <img> must not carry id; src=../assets/... must be wrapped in <div id="picture-NNN">
    for m in IMG_TAG_RE.finditer(html):
        tag = m.group(0)
        src_m = IMG_SRC_ATTR_RE.search(tag)
        if not src_m:
            continue
        src = src_m.group(1)
        if not src.startswith("../assets/"):
            continue
        if IMG_ID_ATTR_RE.search(tag):
            reasons.append(
                f"<img> with src={src} carries id attribute (id must be on outer wrapper div)"
            )
            break
        # Look backward for the immediately-preceding open tag
        before = html[: m.start()]
        prev_open = None
        for om in re.finditer(r"<([a-zA-Z][a-zA-Z0-9]*)\b[^>]*>", before):
            prev_open = om
        if prev_open is None:
            reasons.append(f"<img src={src}> has no preceding parent open tag")
            break
        prev_tag = prev_open.group(0)
        prev_name = prev_open.group(1).lower()
        # Whitespace/comment-only between parent open and img is fine
        gap = before[prev_open.end() :]
        gap_clean = re.sub(r"<!--.*?-->", "", gap, flags=re.DOTALL).strip()
        if gap_clean:
            reasons.append(
                f"<img src={src}> direct parent is not <div id=\"picture-NNN\">; "
                f"intervening content found between parent open and <img>"
            )
            break
        if prev_name != "div":
            reasons.append(
                f"<img src={src}> direct parent must be <div>, got <{prev_name}>"
            )
            break
        id_m = re.search(r'\bid\s*=\s*["\']([^"\']+)["\']', prev_tag)
        if not id_m or not PICTURE_ID_RE.match(id_m.group(1)):
            actual = id_m.group(1) if id_m else "(no id)"
            reasons.append(
                f"<img src={src}> parent <div> id must match picture-NNN, got {actual!r}"
            )
            break
        # parent div must carry inline style with both width and height —
        # downstream extraction keys off these; missing them drops the image
        style_m = re.search(r'\bstyle\s*=\s*["\']([^"\']*)["\']', prev_tag)
        style_val = (style_m.group(1) if style_m else "").lower()
        if "width" not in style_val or "height" not in style_val:
            got = style_m.group(1) if style_m else "(no style)"
            reasons.append(
                f"<img src={src}> parent <div id=picture-NNN> must have inline style "
                f'with both width and height (e.g. style="width:100%;height:100%;"), got {got!r}'
            )
            break
        # parent div must carry class="picture" — downstream selects on it
        class_m = re.search(r'\bclass\s*=\s*["\']([^"\']*)["\']', prev_tag)
        classes = class_m.group(1).split() if class_m else []
        if "picture" not in classes:
            got = class_m.group(1) if class_m else "(no class)"
            reasons.append(
                f"<img src={src}> parent <div id=picture-NNN> must carry "
                f'class="picture", got class={got!r}'
            )
            break
    # 2) CSS background-image url(../assets/...) is forbidden for real assets
    bg_m = ASSET_URL_BG_RE.search(html)
    if bg_m:
        reasons.append(
            "background-image referencing ../assets/ is forbidden (use <img> render unit): "
            + bg_m.group(0)[:120]
        )


def has_valid_open(tag_id: str, html: str) -> tuple[bool, str]:
    """Check that `<... id="tag_id" ...>` exists and is not followed by a stray
    `style=` text node (which means the open tag got split apart)."""
    m = re.search(
        r'<[a-zA-Z]+\b[^>]*\bid=["\']' + re.escape(tag_id) + r'["\'][^>]*>',
        html,
        re.DOTALL,
    )
    if not m:
        return False, f'open tag with id="{tag_id}" not found'
    tail = html[m.end() : m.end() + 400]
    if re.match(r'\s*<!--[^>]*-->\s*style\s*=', tail):
        return False, f'id="{tag_id}" open tag followed by stray style= text node'
    if re.match(r'\s*style\s*=\s*["\']', tail):
        return False, f'id="{tag_id}" open tag followed by stray style= text node'
    return True, "ok"


def check_background_placement(html: str, reasons: list[str]) -> None:
    """Background declarations may only appear inside `#bg` or its descendants.
    Outer containers (<body>, .wrapper/.page/.slide/.container, #ct, #footer)
    must NOT declare background / background-color / background-image."""
    # 1) <body> open tag inline style
    body_open = re.search(r"<body\b[^>]*>", html, re.IGNORECASE)
    if body_open and BG_DECL_RE.search(body_open.group(0)):
        reasons.append("background on <body> (must be in #bg)")
    # 2) any tag whose id is ct/footer or class contains wrapper/page/slide/container
    if not any(r.startswith("background on outer container") for r in reasons):
        for m in re.finditer(r"<[a-zA-Z]+\b[^>]*>", html):
            tag = m.group(0)
            if OUTER_ATTR_RE.search(tag) and BG_DECL_RE.search(tag):
                reasons.append(
                    "background on outer container (must be in #bg): " + tag[:120]
                )
                break
    # 3) <style> block rules whose selectors target outer containers
    for sty in re.findall(
        r"<style\b[^>]*>(.*?)</style>", html, re.DOTALL | re.IGNORECASE
    ):
        sty_violation = False
        for rule in re.finditer(r"([^{}]+)\{([^}]*)\}", sty):
            selectors, decls = rule.group(1), rule.group(2)
            if not BG_DECL_RE.search(decls):
                continue
            for sel in selectors.split(","):
                s = sel.strip()
                if not s:
                    continue
                if OUTER_SELECTOR_RE.search(s) and "#bg" not in s:
                    reasons.append(
                        f"background in <style> selector targeting outer: {s[:80]}"
                    )
                    sty_violation = True
                    break
            if sty_violation:
                break
        if sty_violation:
            break


def check_template_placeholders(html: str, reasons: list[str]) -> None:
    """Strip <script>/<style>/<!-- --> and tags, then look for residual
    template placeholder strings that should have been replaced."""
    visible = re.sub(
        r"<script\b[^>]*>.*?</script>", " ", html, flags=re.DOTALL | re.IGNORECASE
    )
    visible = re.sub(
        r"<style\b[^>]*>.*?</style>", " ", visible, flags=re.DOTALL | re.IGNORECASE
    )
    visible = re.sub(r"<!--.*?-->", " ", visible, flags=re.DOTALL)
    visible = re.sub(r"<[^>]+>", " ", visible)
    for pat in PLACEHOLDER_PATTERNS:
        if re.search(pat, visible, re.IGNORECASE):
            reasons.append(f"template placeholder text not cleared: /{pat}/")
            break


def check_banned_visual_props(html: str, reasons: list[str]) -> None:
    """Forbid CSS the static parser can't reproduce: background-clip:text gradient
    text, clip-path/mask/filter/backdrop-filter/mix-blend-mode, 3D-or-rotate
    transforms, and calc()/viewport-unit/container-query/animation layout features.
    <script> is stripped first so ECharts option strings are never scanned."""
    css = SCRIPT_BLOCK_RE.sub(" ", html)
    if GRADIENT_TEXT_CLIP_RE.search(css):
        reasons.append(
            "background-clip:text gradient text is forbidden (static parser drops it); use solid color"
        )
    m = BANNED_VISUAL_PROP_RE.search(css) or BANNED_TRANSFORM_RE.search(css)
    if m:
        reasons.append(
            "forbidden visual CSS prop (clip-path/mask/filter/backdrop-filter/"
            "mix-blend-mode/3D-or-rotate transform): " + m.group(0).strip()[:80]
        )
    m2 = BANNED_LAYOUT_RE.search(css)
    if m2:
        reasons.append(
            "forbidden layout feature (calc/viewport-unit/container-query/animation): "
            + m2.group(0).strip()[:80]
        )


def check_table_features(html: str, reasons: list[str]) -> None:
    """Tables must be standard equal-column grids: no rowspan/colspan/nested table."""
    m = re.search(r"<(?:td|th)\b[^>]*\b(rowspan|colspan)\s*=", html, re.IGNORECASE)
    if m:
        reasons.append(f"table uses {m.group(1).lower()} (rowspan/colspan forbidden)")
        return
    for tm in re.finditer(r"<table\b[^>]*>(.*?)</table>", html, re.DOTALL | re.IGNORECASE):
        if re.search(r"<table\b", tm.group(1), re.IGNORECASE):
            reasons.append("nested <table> forbidden")
            break


def lint(output_path: Path, template_path: Path) -> tuple[int, dict]:
    t = output_path.read_text(encoding="utf-8", errors="ignore")
    tt = template_path.read_text(encoding="utf-8", errors="ignore")
    reasons: list[str] = []

    # Basic integrity
    if "</html>" not in t.lower():
        reasons.append("missing </html>")
    if len(t) < 500:
        reasons.append(f"too short len={len(t)}")

    # #bg / #ct open tags must exist and not be busted
    for anchor in ("bg", "ct"):
        ok, why = has_valid_open(anchor, t)
        if not ok:
            reasons.append(why)

    # Fixed template images must persist unchanged — EXCEPT
    # ../htmls_png/fallback.png, which is the fillable image-slot placeholder:
    # it must be replaced by a real ../assets/<asset_id> image (when asset_map
    # covers the slot) or the <img> dropped (when no asset), never preserved.
    for fixed_src in re.findall(
        r'<img[^>]+src=["\'](\.\./(?:htmls_png|user)/[^"\']+)["\']', tt
    ):
        if fixed_src.endswith("/fallback.png"):
            continue
        if fixed_src not in t:
            reasons.append(f"fixed template img src missing: {fixed_src}")

    # The fillable placeholder must be resolved: residual fallback.png in the
    # output means the image slot was left unfilled (neither filled from
    # asset_map nor removed).
    if re.search(r'src=["\']\.\./htmls_png/fallback\.png["\']', t):
        reasons.append(
            "unfilled image slot: ../htmls_png/fallback.png left in output "
            "(fill from asset_map_page.image_assets or drop the <img>)"
        )

    # No new <script> if template had none
    tpl_has_script = bool(re.search(r"<script\b", tt, re.IGNORECASE))
    out_has_script = bool(re.search(r"<script\b", t, re.IGNORECASE))
    if out_has_script and not tpl_has_script:
        reasons.append("output added <script> absent from template")

    # No duplicate style= on same element (catches <div style="..." style="...">)
    for m in re.finditer(r"<[a-zA-Z][^>]*>", t):
        tag = m.group(0)
        if tag.count(" style=") > 1 or tag.count("\tstyle=") > 1:
            reasons.append("duplicate style= on element: " + tag[:80])
            break

    check_background_placement(t, reasons)
    check_template_placeholders(t, reasons)
    check_picture_wrapper(t, reasons)
    check_banned_visual_props(t, reasons)
    check_table_features(t, reasons)
    check_theme_links(t, tt, reasons)
    check_theme_vars(t, tt, reasons)

    payload: dict = {"output": str(output_path), "len": len(t)}
    if reasons:
        payload["status"] = "fail"
        payload["reasons"] = reasons
        return 2, payload
    payload["status"] = "ok"
    return 0, payload


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Lint a stage 5 page HTML against its reference template."
    )
    parser.add_argument(
        "--output",
        required=True,
        help="Path to the generated page HTML (e.g. htmls/page_001.html).",
    )
    parser.add_argument(
        "--template",
        required=True,
        help="Path to the reference template HTML this page was based on.",
    )
    args = parser.parse_args()

    out_path = Path(args.output)
    tpl_path = Path(args.template)
    if not out_path.is_file():
        print(
            json.dumps(
                {
                    "status": "fail",
                    "output": str(out_path),
                    "reasons": ["output file not found"],
                },
                ensure_ascii=False,
            )
        )
        return 2
    if not tpl_path.is_file():
        print(
            json.dumps(
                {
                    "status": "fail",
                    "output": str(out_path),
                    "reasons": [f"template file not found: {tpl_path}"],
                },
                ensure_ascii=False,
            )
        )
        return 2

    code, payload = lint(out_path, tpl_path)
    print(json.dumps(payload, ensure_ascii=False))
    return code


if __name__ == "__main__":
    sys.exit(main())
