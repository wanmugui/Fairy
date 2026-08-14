"""Fairy voice service: MOSS-TTS output + SenseVoice streaming STT."""
import asyncio, base64, json, os, sys, threading, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import numpy as np

REPO = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(REPO / "voice"))

from tts_engine import MossTTSNanoEngine

TTS_DIR = REPO / "models" / "voice" / "MOSS-TTS-Nano-100M-ONNX"
CODEC_DIR = REPO / "models" / "voice" / "MOSS-Audio-Tokenizer-Nano-ONNX"

_tts = None
_tts_lock = threading.Lock()
_stt = None
_stt_lock = threading.Lock()


def get_tts():
    global _tts
    if _tts is None:
        with _tts_lock:
            if _tts is None:
                print("[voice] loading MOSS-TTS...", flush=True)
                _tts = MossTTSNanoEngine(str(TTS_DIR), str(CODEC_DIR))
                print("[voice] TTS ready", flush=True)
    return _tts


def get_stt():
    global _stt
    if _stt is None:
        with _stt_lock:
            if _stt is None:
                print("[voice] loading SenseVoice STT...", flush=True)
                from sense_voice_streaming_asr.sense_voice_streaming_asr import (
                    SenseVoiceStreamingASR, StreamingASRConfig,
                )
                from sense_voice_streaming_asr.model_data import SenseVoiceModel, VadModel
                _stt = {
                    "asr": SenseVoiceStreamingASR(SenseVoiceModel(use_cuda=False), VadModel(), StreamingASRConfig(lang="zh")),
                    "events": [],
                    "cb": None,
                }
                def cb(etype, text):
                    _stt["events"].append((etype.name, text))
                _stt["asr"].set_on_event_callback(cb)
                print("[voice] STT ready", flush=True)
    return _stt


class HTTPHandler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass

    def _cors(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

    def do_OPTIONS(self):
        self.send_response(204)
        self._cors()
        self.end_headers()

    def _json(self, obj, code=200):
        body = json.dumps(obj, ensure_ascii=False).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self._cors()
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            self._json({"ok": True, "tts": _tts is not None, "stt": _stt is not None})
            return
        self._json({"error": "not found"}, 404)

    def do_POST(self):
        if self.path != "/api/tts":
            self._json({"error": "not found"}, 404)
            return
        try:
            length = int(self.headers.get("Content-Length", 0))
            data = json.loads(self.rfile.read(length).decode("utf-8"))
            text = (data.get("text") or "").strip()
            if not text:
                self._json({"error": "empty text"}, 400)
                return
            t0 = time.time()
            wav, sr, _ = get_tts().synthesize(text, max_frames=300)
            if wav.shape[1] == 0:
                self._json({"error": "no audio", "text": text}, 500)
                return
            interleaved = np.zeros(wav.shape[1] * 2, dtype=np.float32)
            interleaved[0::2] = wav[0]
            interleaved[1::2] = wav[1]
            b64 = base64.b64encode(interleaved.tobytes()).decode("ascii")
            self._json({
                "text": text,
                "audio_b64": b64,
                "sample_rate": sr,
                "seconds": round(wav.shape[1] / sr, 2),
                "synthesize_sec": round(time.time() - t0, 2),
            })
        except Exception as e:
            self._json({"error": str(e)}, 500)


async def handle_stt(ws):
    stt = get_stt()
    print("[voice] STT client connected", flush=True)
    async for raw in ws:
        try:
            if isinstance(raw, bytes):
                arr = np.frombuffer(raw, dtype=np.float32)
                if arr.size == 0:
                    continue
                stt["events"].clear()
                stt["asr"].accept_audio(arr)
                for etype, text in stt["events"]:
                    if text and text.strip():
                        await ws.send(json.dumps({"type": etype.lower(), "text": text.strip()}, ensure_ascii=False))
            else:
                msg = json.loads(raw)
                if msg.get("type") == "reset":
                    stt["events"].clear()
                elif msg.get("type") == "end":
                    stt["events"].clear()
                    try:
                        stt["asr"].finalize_utterance()
                    except Exception:
                        pass
                    for etype, text in stt["events"]:
                        if etype == "FINAL_RESULT" and text and text.strip():
                            await ws.send(json.dumps({"type": "final", "text": text.strip()}, ensure_ascii=False))
        except Exception as e:
            print("[voice] stt error:", e, flush=True)


def run_http():
    srv = ThreadingHTTPServer(("127.0.0.1", 8787), HTTPHandler)
    srv.serve_forever()


async def main():
    print("[voice] HTTP :8787  WS :8788", flush=True)
    threading.Thread(target=run_http, daemon=True).start()
    async with __import__("websockets").serve(handle_stt, "127.0.0.1", 8788, max_size=4_000_000):
        await asyncio.Future()


if __name__ == "__main__":
    asyncio.run(main())
