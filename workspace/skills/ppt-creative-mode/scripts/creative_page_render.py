#!/usr/bin/env python3
"""消费切片 JSON，调用 creative_page_render API，把返回的 base64 图片保存到 output_png_path。

用法:
    python creative_page_render.py --input <deck_dir>/pages/page_xxx.input.json

脚本内置 3 次重试（指数退避 2s/4s/8s）。API 端点优先从 CREATIVE_RENDER_API_URL 环境变量读取；
若未设置，则使用 BACKEND_TOOL_BASE + /api/agent/tool_call，默认 base 为
https://code-dev-public.xiaohuanxiong.com。

触发重试的情况：连接错误、读超时、HTTP 5xx、响应 code != 0、响应缺 result_page_image_base64。
全部失败则以 exit_code=1 退出，stderr 打印 attempts=3 + last_error。
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import socket
import sys
import time
from contextlib import contextmanager
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

import requests

try:
    import load_env
    load_env.load()
except (ImportError, AttributeError):
    pass


BACKEND_TOOL_BASE = os.getenv('BACKEND_TOOL_BASE', 'https://code-dev-public.xiaohuanxiong.com')
DEFAULT_API_PATH = "/api/agent/tool_call"
DEFAULT_API_URL = f"{BACKEND_TOOL_BASE}{DEFAULT_API_PATH}"
DEFAULT_API_HOST = "code-dev-public.xiaohuanxiong.com"
DEFAULT_API_HOST_IP = "14.103.45.137"
DEFAULT_TIMEOUT = 600
DEFAULT_MAX_ATTEMPTS = 3
BACKOFF_SEQUENCE = (2, 4, 8)  # 最多 3 次尝试 → 2 次间隔


def _api_url() -> str:
    creative_render_api_url = os.environ.get("CREATIVE_RENDER_API_URL")
    if creative_render_api_url:
        return creative_render_api_url

    backend_tool_base = os.environ.get("BACKEND_TOOL_BASE")
    if backend_tool_base:
        normalized_base = backend_tool_base.rstrip("/")
        if normalized_base.endswith(DEFAULT_API_PATH):
            return normalized_base
        return f"{normalized_base}{DEFAULT_API_PATH}"

    return DEFAULT_API_URL


def _host_override_map() -> dict[str, str]:
    overrides = {
        DEFAULT_API_HOST: DEFAULT_API_HOST_IP,
    }
    # env CREATIVE_RENDER_HOST_IP="host=ip" 可追加/覆盖一条 host→IP 固定映射
    # （本地网络解析不到 code-dev 公网域名时，钉到内网网关 172.30.17.27）。
    extra = os.environ.get("CREATIVE_RENDER_HOST_IP", "").strip()
    if extra and "=" in extra:
        host, ip = extra.split("=", 1)
        overrides[host.strip()] = ip.strip()
    return overrides


@contextmanager
def _host_override_for_url(url: str):
    """Temporarily resolve selected API hosts to fixed IPs without changing the URL."""
    hostname = urlparse(url).hostname or ""
    override_ip = _host_override_map().get(hostname)
    if not override_ip:
        yield
        return

    original_getaddrinfo = socket.getaddrinfo

    def _patched_getaddrinfo(host: str, *args, **kwargs):
        if host == hostname:
            return original_getaddrinfo(override_ip, *args, **kwargs)
        return original_getaddrinfo(host, *args, **kwargs)

    socket.getaddrinfo = _patched_getaddrinfo
    try:
        yield
    finally:
        socket.getaddrinfo = original_getaddrinfo


_REQUIRED_SLICE_FIELDS = (
    "ppt_id",
    "ppt_title",
    "page_number",
    "page_outline",
    "page_style_md",
    "output_png_path",
)


def _load_slice(path: Path) -> dict:
    if not path.is_file():
        raise SystemExit(f"[creative_page_render] slice not found: {path}")
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise SystemExit(f"[creative_page_render] slice invalid json: {exc}") from exc

    missing = [f for f in _REQUIRED_SLICE_FIELDS if not data.get(f) and data.get(f) != 0]
    # page_number 可以是 int；page_outline 可以是 dict；允许 0？—— 禁止
    if data.get("page_number") in (None, 0):
        missing.append("page_number")
    if missing:
        raise SystemExit(
            f"[creative_page_render] slice missing fields: {sorted(set(missing))}"
        )
    return data


def _build_payload(slice_obj: dict) -> dict:
    arguments = {
        "mode": "stateless",
        "ppt_id": slice_obj["ppt_id"],
        "ppt_title": slice_obj["ppt_title"],
        "page_num": slice_obj["page_number"],
        "page_outline": json.dumps(slice_obj["page_outline"], ensure_ascii=False),
        "page_style": slice_obj["page_style_md"],
    }
    return {
        "tool_call_id": f"call_creative_page_render_{int(slice_obj['page_number']):03d}",
        "tool_name": "creative_page_render",
        "arguments": json.dumps(arguments, ensure_ascii=False),
    }


def _extract_base64(resp_json: dict[str, Any]) -> str:
    code = resp_json.get("code")
    if code != 0:
        raise RuntimeError(f"api code={code} msg={resp_json.get('msg')}")

    data = resp_json.get("data") or {}
    result_raw = data.get("result")
    if not result_raw:
        raise RuntimeError("response missing data.result")

    result = json.loads(result_raw)
    b64 = result.get("result_page_image_base64")
    if not b64:
        raise RuntimeError("result missing result_page_image_base64")

    if b64.startswith("data:") and "," in b64:
        b64 = b64.split(",", 1)[1]
    return b64


def _one_attempt(url: str, body: dict, timeout: int) -> str:
    with _host_override_for_url(url):
        resp = requests.post(
            url,
            headers={"Content-Type": "application/json"},
            data=json.dumps(body, ensure_ascii=False).encode("utf-8"),
            timeout=timeout,
        )
    resp.raise_for_status()
    return _extract_base64(resp.json())


def run(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True, help="page_xxx.input.json 绝对路径")
    parser.add_argument("--timeout", type=int, default=DEFAULT_TIMEOUT, help="单次 HTTP 超时（秒）")
    parser.add_argument(
        "--max-attempts",
        type=int,
        default=DEFAULT_MAX_ATTEMPTS,
        help="最大尝试次数（含首次）",
    )
    args = parser.parse_args(argv)

    slice_path = Path(args.input)
    slice_obj = _load_slice(slice_path)
    body = _build_payload(slice_obj)

    url = _api_url()
    last_error: Exception | None = None
    b64: str | None = None

    for attempt in range(1, args.max_attempts + 1):
        try:
            b64 = _one_attempt(url, body, args.timeout)
            break
        except Exception as exc:  # noqa: BLE001 — 捕获所有异常触发重试
            last_error = exc
            if attempt < args.max_attempts:
                backoff = BACKOFF_SEQUENCE[min(attempt - 1, len(BACKOFF_SEQUENCE) - 1)]
                time.sleep(backoff)

    if b64 is None:
        page_number = slice_obj.get("page_number")
        print(
            f"[creative_page_render] page_number={page_number} attempts={args.max_attempts} "
            f"last_error={type(last_error).__name__}: {last_error}",
            file=sys.stderr,
        )
        return 1

    image_bytes = base64.b64decode(b64)
    output_path = Path(slice_obj["output_png_path"]).resolve()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_bytes(image_bytes)

    print(
        json.dumps(
            {
                "status": "ok",
                "output_png_path": str(output_path),
                "page_number": slice_obj["page_number"],
                "bytes": len(image_bytes),
            },
            ensure_ascii=False,
        )
    )
    return 0


if __name__ == "__main__":
    from _stage_status import cli_value, run_main
    _input = Path(cli_value("--input")).resolve()
    sys.exit(run_main(run, _input.parent.parent, "generating_pages", "render_creative_page"))
