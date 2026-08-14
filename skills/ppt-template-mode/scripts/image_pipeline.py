#!/usr/bin/env python3
"""并发下载 + 并发检查搜图候选，给每个图槽选出一张可用图落到 assets/。

候选的「获取 + 检查」都并发（默认 10）。每个候选既可以是 http(s) URL（脚本并发下载），
也可以是**已经下载好的本地文件路径**（脚本直接读字节）——所以无论上游 image_search 走 download=false
取 URL、还是 download=true 把候选下到本地，本脚本都吃；检查这一步是统一入口、必须经过它：
  - URL 候选：线程池并发走 fetch_url(file_mode=stateless) 让后端下载并返回 base64 字节
    （沙箱直连外网图床 DNS/网络不通，统一借后端工具下载更稳）；本地路径候选：直接读文件字节。
  - 检查并发：同一线程池里，拿到字节立刻跑 4 条代码级硬检查 + 1 次 image_filter 视觉检查。
代码级硬检查（保留原口径，纯代码判断）：
  1. 文件大小 >= 8KB
  2. 真实分辨率 宽>=200 且 高>=200
  3. 像素多样性：64 个随机像素(seed=0)按 (R//16,G//16,B//16) 归桶，unique 桶 >= 8
  4. sha256 去重：同一次运行里两个图槽不得选到字节完全相同的图（选取阶段跨槽去重）
视觉检查（从「agent 自己看图」搬到 API）：POST image_filter，prompt 要求只返回 {"pass":bool,"reason":str}。
  判定三态：pass / reject / error（调用失败）。**error ≠ reject**：调用成功且语义不通过的候选必死；
  调用失败的候选不立即入选也不丢弃——只有当该槽**没有任何视觉确认通过**的候选时，才从「调用失败
  但 4 条代码检查全过」的候选里按原顺序兜底入选（reason 记 vision_unavailable_default_pass）。
  这样接口抖动既不拉低有视觉确认时的质量门槛，也不会把槽位整个打空去触发补搜/生图。

用法:
    python image_pipeline.py --spec <spec.json> [--concurrency 10]

spec.json 结构（asset-planning subagent 用 image_search(download=false) 拿到候选 URL 后自行拼出）:
    {
      "deck_dir": "/abs/deck",
      "concurrency": 10,
      "slots": [
        {"asset_id":"page_003_ill_02","slot_id":"picture-007","page_id":"page_003",
         "asset_kind":"illustration",
         "check_prompt":"判断这张图是否可用作 ... 只返回 JSON {\"pass\":true/false,\"reason\":\"...\"}",
         "candidates":["https://a.jpg","https://b.png","https://c.webp"]}
      ]
    }

stdout 输出一行 JSON:
    {"status":"ok","results":[{"asset_id":...,"slot_id":...,"page_id":...,"asset_kind":...,
                               "source":"image_search","local_path":".../assets/page_003_ill_02.jpg"|null,
                               "reason":"..."}]}
退出码恒为 0（除非 spec 不可读/非法）；单个图槽失败只体现在 local_path=null，不影响其它槽。
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import io
import json
import os
import random
import sys
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from urllib.parse import urlparse

from PIL import Image

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from _tool_call import ToolCallError, call_tool  # noqa: E402

try:
    import load_env
    load_env.load()
except (ImportError, AttributeError):
    pass

MIN_BYTES = 8 * 1024
MIN_W = MIN_H = 200
MIN_COLOR_BUCKETS = 8
FETCH_TIMEOUT = 15
VISION_MAX_SIDE = 1024          # 送 image_filter 前缩到最长边 <=1024，控住 5MB 上限
VISION_JPEG_QUALITY = 85
DEFAULT_CONCURRENCY = 16
MAX_CANDIDATES_PER_SLOT = 5   # 每槽最多检查这么多候选：给够候选让一轮就能选中，避免上游补搜重跑
_EXT_BY_CT = {
    "image/jpeg": "jpg",
    "image/jpg": "jpg",
    "image/png": "png",
    "image/webp": "webp",
    "image/gif": "gif",
}


def _ext_from(url: str, content_type: str | None) -> str:
    if content_type:
        base = content_type.split(";")[0].strip().lower()
        if base in _EXT_BY_CT:
            return _EXT_BY_CT[base]
    suffix = Path(urlparse(url).path).suffix.lower().lstrip(".")
    if suffix in ("jpg", "jpeg"):
        return "jpg"
    if suffix in ("png", "webp", "gif"):
        return suffix
    return "jpg"


def _pixel_diversity_ok(img: Image.Image) -> bool:
    rgb = img.convert("RGB")
    w, h = rgb.size
    rnd = random.Random(0)
    buckets = set()
    for _ in range(64):
        x = rnd.randrange(w)
        y = rnd.randrange(h)
        r, g, b = rgb.getpixel((x, y))
        buckets.add((r // 16, g // 16, b // 16))
    return len(buckets) >= MIN_COLOR_BUCKETS


def _code_checks(content: bytes) -> tuple[bool, str, Image.Image | None]:
    if len(content) < MIN_BYTES:
        return False, "pixel_placeholder(<8KB)", None
    try:
        img = Image.open(io.BytesIO(content))
        img.load()
    except Exception as exc:  # noqa: BLE001
        return False, f"undecodable_image({type(exc).__name__})", None
    w, h = img.size
    if w < MIN_W or h < MIN_H:
        return False, f"pixel_too_small({w}x{h})", None
    if not _pixel_diversity_ok(img):
        return False, "pixel_monotone", None
    return True, "", img


def _vision_b64(img: Image.Image) -> str:
    rgb = img.convert("RGB")
    w, h = rgb.size
    scale = min(1.0, VISION_MAX_SIDE / max(w, h))
    if scale < 1.0:
        rgb = rgb.resize((max(1, int(w * scale)), max(1, int(h * scale))))
    buf = io.BytesIO()
    rgb.save(buf, format="JPEG", quality=VISION_JPEG_QUALITY)
    return base64.b64encode(buf.getvalue()).decode("ascii")


def _vision_pass(check_prompt: str, img: Image.Image, call_id: str) -> tuple[str, str]:
    """视觉检查三态：'pass' / 'reject' / 'error'（调用失败）。

    error ≠ reject：调用成功时语义不通过肯定不通过；调用失败时语义仍可能通过——
    error 候选由选图阶段在「该槽没有视觉确认通过的候选」时按序兜底，避免打空槽位触发补搜。
    """
    if not check_prompt:
        return "pass", "no_vision_prompt"
    try:
        result = call_tool(
            "image_filter",
            {"prompt": check_prompt, "image_base64": _vision_b64(img)},
            tool_call_id=call_id,
        )
    except ToolCallError as exc:
        return "error", f"vision_unavailable({exc})"
    verdict = result.get("pass")
    if verdict is None:
        verdict = result.get("ok")
    reason = str(result.get("reason", ""))
    if bool(verdict):
        return "pass", reason or "vision_pass"
    return "reject", reason or "vision_fail"


def _evaluate_candidate(task: dict) -> dict:
    """下载 + 代码检查 + 视觉检查一个候选，返回 verdict（线程池任务体）。

    全程兜底：单个候选的任何异常都只让这个候选失败，绝不顺着 pool.map 把整批炸掉。
    """
    cand = task["url"]  # 可能是 http(s) URL，也可能是已经下载好的本地文件路径
    out = {**task, "ok": False, "reason": "", "tmp_path": None, "sha": None, "ext": "jpg"}
    try:
        if cand.startswith("http://") or cand.startswith("https://"):
            # 走后端 fetch_url 下载：file_mode=stateless 不写沙盒，直接返回文件 base64。
            # 图床 URL 多以 .jpg/.png 等扩展名结尾 → 命中 fetch_url 的文件下载分支拿到 file_base64；
            # 若 URL 无可识别扩展名而回退到网页抓取分支，则拿不到 file_base64，按下载失败处理。
            try:
                result = call_tool(
                    "fetch_url",
                    {"url": cand, "file_mode": "stateless"},
                    tool_call_id=f"call_fetch_url_{task['asset_id']}_{task['cand_idx']}",
                    timeout=FETCH_TIMEOUT,
                )
            except ToolCallError as exc:
                out["reason"] = f"download_fail({exc}) url={cand}"
                return out
            b64 = result.get("file_base64")
            if not b64:
                # 未命中文件下载分支（无扩展名/非文件 URL）：拿不到文件字节
                out["reason"] = f"download_fail(no_file_base64) url={cand}"
                return out
            try:
                content = base64.b64decode(b64)
            except Exception as exc:  # noqa: BLE001
                out["reason"] = f"download_fail(b64_decode:{type(exc).__name__}) url={cand}"
                return out
            ext_hint = _ext_from(result.get("file_name") or cand, None)
        else:
            p = Path(cand)
            if not p.is_file():
                out["reason"] = "local_missing"
                return out
            content = p.read_bytes()
            ext_hint = _ext_from(cand, None)

        ok, reason, img = _code_checks(content)
        if not ok:
            out["reason"] = reason
            return out
        assert img is not None

        v_state, v_reason = _vision_pass(
            task.get("check_prompt", ""), img,
            f"call_image_filter_{task['asset_id']}_{task['cand_idx']}",
        )
        if v_state == "reject":
            out["reason"] = f"vision_reject:{v_reason}"
            return out

        # pass 与 error 都物化（error 候选留作兜底，不丢弃也不立即入选）
        out["ext"] = ext_hint
        out["sha"] = hashlib.sha256(content).hexdigest()
        tmp_path = task["cand_dir"] / f"{task['asset_id']}__c{task['cand_idx']}.{out['ext']}"
        tmp_path.write_bytes(content)
        out["tmp_path"] = str(tmp_path)
        out["ok"] = True
        out["vision_state"] = v_state
        out["reason"] = v_reason
        return out
    except Exception as exc:  # noqa: BLE001 — 单候选兜底，不炸整批
        out["ok"] = False
        out["reason"] = f"eval_error({type(exc).__name__})"
        return out


def run(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--spec", required=True, help="候选规格 JSON 绝对路径")
    parser.add_argument("--concurrency", type=int, default=DEFAULT_CONCURRENCY)
    args = parser.parse_args(argv)

    spec_path = Path(args.spec)
    if not spec_path.is_file():
        print(json.dumps({"status": "fail", "reason": f"spec not found: {spec_path}"}))
        return 1
    try:
        spec = json.loads(spec_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        print(json.dumps({"status": "fail", "reason": f"spec invalid json: {exc}"}))
        return 1

    deck_dir = Path(spec["deck_dir"])
    assets_dir = deck_dir / "assets"
    cand_dir = assets_dir / "_cand"
    assets_dir.mkdir(parents=True, exist_ok=True)
    cand_dir.mkdir(parents=True, exist_ok=True)
    concurrency = int(spec.get("concurrency") or args.concurrency)

    slots = spec.get("slots", [])
    cap = int(spec.get("max_candidates") or MAX_CANDIDATES_PER_SLOT)
    tasks: list[dict] = []
    for s_idx, slot in enumerate(slots):
        for c_idx, url in enumerate((slot.get("candidates") or [])[:cap]):
            tasks.append(
                {
                    "slot_idx": s_idx,
                    "cand_idx": c_idx,
                    "url": url,
                    "asset_id": slot["asset_id"],
                    "check_prompt": slot.get("check_prompt", ""),
                    "cand_dir": cand_dir,
                }
            )

    verdicts: list[dict] = []
    if tasks:
        with ThreadPoolExecutor(max_workers=max(1, concurrency)) as pool:
            verdicts = list(pool.map(_evaluate_candidate, tasks))

    # 按 slot 收集候选 verdict（保持候选原顺序）
    by_slot: dict[int, list[dict]] = {i: [] for i in range(len(slots))}
    for v in verdicts:
        by_slot[v["slot_idx"]].append(v)
    for lst in by_slot.values():
        lst.sort(key=lambda v: v["cand_idx"])

    used_sha: set[str] = set()
    results: list[dict] = []
    for s_idx, slot in enumerate(slots):
        chosen = None
        fallback_reason = ""
        reasons = []
        # 第一遍：只选「视觉确认通过」的候选（按原顺序 + 跨槽 sha 去重）
        for v in by_slot.get(s_idx, []):
            if not v["ok"]:
                reasons.append(f"c{v['cand_idx']}:{v['reason']}")
                continue
            if v["sha"] in used_sha:
                reasons.append(f"c{v['cand_idx']}:duplicated")
                continue
            if v.get("vision_state") == "pass":
                chosen = v
                break
        # 第二遍兜底：该槽没有任何视觉确认通过的候选时，从「image_filter 调用失败
        # 但代码检查全过」的候选里按序选一张——调用失败≠语义不通过，避免打空槽位触发补搜
        if chosen is None:
            for v in by_slot.get(s_idx, []):
                if v["ok"] and v.get("vision_state") == "error" and v["sha"] not in used_sha:
                    chosen = v
                    fallback_reason = "vision_unavailable_default_pass：image_filter 调用失败，按语义默认通过兜底（4 条代码检查已过）"
                    break
        record = {
            "asset_id": slot["asset_id"],
            "slot_id": slot["slot_id"],
            "page_id": slot.get("page_id"),
            "asset_kind": slot.get("asset_kind"),
            "source": "image_search",
            "local_path": None,
            "reason": "",
        }
        if chosen is None:
            record["reason"] = (
                "; ".join(reasons) if reasons else "no_candidates"
            )[:300]
        else:
            ext = chosen["ext"]
            final_path = assets_dir / f"{slot['asset_id']}.{ext}"
            final_path.write_bytes(Path(chosen["tmp_path"]).read_bytes())
            used_sha.add(chosen["sha"])
            record["local_path"] = str(final_path)
            record["reason"] = (fallback_reason or chosen["reason"])[:200]
        results.append(record)

    # 清理候选临时目录
    for f in cand_dir.glob("*"):
        try:
            f.unlink()
        except OSError:
            pass
    try:
        cand_dir.rmdir()
    except OSError:
        pass

    print(json.dumps({"status": "ok", "results": results}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    from _stage_status import cli_value, run_main
    _spec = Path(cli_value("--spec")).resolve()
    sys.exit(run_main(run, _spec.parent.parent, "preparing_assets", "process_assets"))
