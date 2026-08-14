#!/usr/bin/env python3
"""POST /api/agent/tool_call 的薄客户端，供 image_pipeline.py / html_page_generate_batch.py /
html_page_review_batch.py 复用。

端点解析优先级：
    PPT_TOOL_API_URL          # 完整 URL，直接用
    PPT_TOOL_API_BASE + path  # 只给 base
    BACKEND_TOOL_BASE  + path  # 兼容 creative 的环境变量
    DEFAULT_BASE       + path  # 默认 code-stage

host→IP 固定映射：沙箱里这些 host 的公网 DNS 解析不到可达 IP（实测 code-stage 的公网 DNS 指向私网
172.30.x、沙箱直连超时；钉到网关 IP 才通）。按 `_HOST_PIN` 把 host 钉到可达 IP，env `PPT_TOOL_HOST_IP="host=ip"`
可追加/覆盖一条。钉 DNS 不改 URL，TLS SNI / Host 头仍是原 host，证书照常校验。

`arguments` 必须是 JSON 字符串；返回的 `data.result` 也是 JSON 字符串——本模块把这两层都处理掉，
调用方拿到的是已解析好的 dict。HTTP 4xx/5xx 或 code!=0 时，异常信息里带响应正文片段，便于定位。
内置 3 次重试（指数退避 2s/4s/8s）。
"""

from __future__ import annotations

import json
import os
import socket
import time
from typing import Any

import requests

try:
    import load_env

    load_env.load()
except (ImportError, AttributeError):
    pass

DEFAULT_BASE = "https://code-stage.xiaohuanxiong.com"
API_PATH = "/api/agent/tool_call"
DEFAULT_TIMEOUT = 600
DEFAULT_MAX_ATTEMPTS = 3
BACKOFF_SEQUENCE = (2, 4, 8)

# 沙箱里公网 DNS 不可达时，把 host 钉到可达网关 IP。env PPT_TOOL_HOST_IP="host=ip" 可追加/覆盖。
_HOST_PIN = {
    "code-stage.xiaohuanxiong.com": "180.153.172.2",
}


class ToolCallError(RuntimeError):
    """tool_call 接口层面的失败（HTTP、业务 code、解析），message 里尽量带响应正文。"""


def _host_pin_map() -> dict[str, str]:
    pins = dict(_HOST_PIN)
    extra = os.environ.get("PPT_TOOL_HOST_IP", "").strip()
    if extra and "=" in extra:
        host, ip = extra.split("=", 1)
        pins[host.strip()] = ip.strip()
    return pins


def api_url() -> str:
    explicit = os.environ.get("PPT_TOOL_API_URL")
    if explicit:
        return explicit
    base = (
        os.environ.get("PPT_TOOL_API_BASE")
        or os.environ.get("BACKEND_TOOL_BASE")
        or DEFAULT_BASE
    ).rstrip("/")
    if base.endswith(API_PATH):
        return base
    return f"{base}{API_PATH}"


def _install_host_pin() -> None:
    """进程级一次性安装 DNS pin：把 _host_pin_map() 里的 host 解析到固定 IP，其它 host 透传。

    在模块加载时**装一次**（而不是每次请求 patch + finally restore）。批处理脚本用线程池并发调
    本模块时，per-request 的 patch/restore 会让多个线程互相覆盖 `original`、提前把别的线程恢复成
    未 pin 的 getaddrinfo，导致部分请求随机丢失 host pin → 随机超时/失败。一次性安装无 enter/exit、
    幂等、对所有线程一致，没有竞态。不改 URL，TLS SNI / Host 头仍是原 host，证书照常校验。
    """
    pins = _host_pin_map()
    if not pins:
        return
    original = socket.getaddrinfo
    if getattr(original, "_ppt_pinned", False):
        return  # 已装过，幂等

    def patched(host, *args, **kwargs):
        ip = pins.get(host)
        return original(ip, *args, **kwargs) if ip else original(host, *args, **kwargs)

    patched._ppt_pinned = True
    socket.getaddrinfo = patched


_install_host_pin()  # 模块加载时安装一次，进程级、线程安全


def call_tool(
    tool_name: str,
    arguments: dict[str, Any],
    *,
    tool_call_id: str | None = None,
    timeout: int = DEFAULT_TIMEOUT,
    max_attempts: int = DEFAULT_MAX_ATTEMPTS,
) -> dict[str, Any]:
    """调一次 tool_call，返回已解析的 data.result（dict）。失败抛 ToolCallError。"""
    body = {
        "tool_call_id": tool_call_id or f"call_{tool_name}",
        "tool_name": tool_name,
        "arguments": json.dumps(arguments, ensure_ascii=False),
    }
    url = api_url()
    payload = json.dumps(body, ensure_ascii=False).encode("utf-8")
    last_error: Exception | None = None

    for attempt in range(1, max_attempts + 1):
        try:
            resp = requests.post(
                url,
                headers={"Content-Type": "application/json"},
                data=payload,
                timeout=timeout,
            )
            if resp.status_code >= 400:
                raise ToolCallError(f"HTTP {resp.status_code}: {resp.text[:300]}")
            outer = resp.json()
            if outer.get("code") != 0:
                raise ToolCallError(
                    f"api code={outer.get('code')} msg={outer.get('msg')!r} body={resp.text[:200]}"
                )
            result_raw = (outer.get("data") or {}).get("result")
            if not result_raw:
                raise ToolCallError(f"response missing data.result: {resp.text[:200]}")
            return json.loads(result_raw)
        except Exception as exc:  # noqa: BLE001 — 任何异常都触发重试
            last_error = exc
            if attempt < max_attempts:
                time.sleep(
                    BACKOFF_SEQUENCE[min(attempt - 1, len(BACKOFF_SEQUENCE) - 1)]
                )

    raise ToolCallError(
        f"tool_call {tool_name} failed after {max_attempts} attempts: "
        f"{type(last_error).__name__}: {last_error}"
    )
