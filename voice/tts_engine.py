"""MOSS-TTS-Nano-100M-ONNX inference engine (CPU, torch-free)."""
import json, os, time
import numpy as np
import onnxruntime as ort
import sentencepiece as spm


class MossTTSNanoEngine:
    def __init__(self, tts_dir, codec_dir):
        self.tts_dir = tts_dir
        self.codec_dir = codec_dir
        with open(os.path.join(tts_dir, "browser_poc_manifest.json"), "r", encoding="utf-8") as f:
            m = json.load(f)
        with open(os.path.join(tts_dir, "tts_browser_onnx_meta.json"), "r", encoding="utf-8") as f:
            meta = json.load(f)
        with open(os.path.join(codec_dir, "codec_browser_onnx_meta.json"), "r", encoding="utf-8") as f:
            cm = json.load(f)

        cfg = m["tts_config"]
        self.audio_pad = cfg["audio_pad_token_id"]
        self.audio_start = cfg["audio_start_token_id"]
        self.audio_assistant = cfg["audio_assistant_slot_token_id"]
        self.vocab_size = cfg["vocab_size"]
        self.audio_codebook_total = sum(cfg["audio_codebook_sizes"])
        self.mask_size = self.vocab_size + self.audio_codebook_total

        mc = meta["model_config"]
        self.global_layers = mc["global_layers"]
        self.codec_sr = cm["codec_config"]["sample_rate"]
        self.codec_channels = cm["codec_config"]["channels"]

        self.pt = np.array(m["prompt_templates"]["user_prompt_prefix_token_ids"], dtype=np.int64)
        self.par = np.array(m["prompt_templates"]["user_prompt_after_reference_token_ids"], dtype=np.int64)
        self.ap = np.array(m["prompt_templates"]["assistant_prompt_prefix_token_ids"], dtype=np.int64)
        self.pc = np.array(m["builtin_voices"][0]["prompt_audio_codes"], dtype=np.int64)

        self._load_models()
        self._load_tokenizer()

    def _load_tokenizer(self):
        self.sp = spm.SentencePieceProcessor()
        self.sp.Load(os.path.join(self.tts_dir, "tokenizer.model"))

    def _create_session(self, path):
        opts = ort.SessionOptions()
        opts.graph_optimization_level = ort.GraphOptimizationLevel.ORT_ENABLE_ALL
        return ort.InferenceSession(path, opts, providers=["CPUExecutionProvider"])

    def _load_models(self):
        t = time.time()
        self.sess_prefill = self._create_session(os.path.join(self.tts_dir, "moss_tts_prefill.onnx"))
        self.sess_decode = self._create_session(os.path.join(self.tts_dir, "moss_tts_decode_step.onnx"))
        self.sess_fixed = self._create_session(os.path.join(self.tts_dir, "moss_tts_local_fixed_sampled_frame.onnx"))
        self.sess_codec = self._create_session(os.path.join(self.codec_dir, "moss_audio_tokenizer_decode_full.onnx"))
        self._load_sec = time.time() - t

    def synthesize(self, text, max_frames=300):
        t0 = time.time()
        ti = np.array(self.sp.EncodeAsIds(text), dtype=np.int64)
        inp = np.concatenate([self.pt, ti, self.par, self.ap])

        r = np.full((1, len(inp), 17), self.audio_pad, dtype=np.int32)
        r[0, :, 0] = inp.astype(np.int32)
        o = self.sess_prefill.run(None, {"input_ids": r, "attention_mask": np.ones((1, len(inp)), dtype=np.int32)})
        h, kv = o[0], {}
        for i in range(self.global_layers):
            kv[f"k{i}"] = o[1 + i * 2]
            kv[f"v{i}"] = o[2 + i * 2]
        sl = len(inp)

        rm = np.zeros((16, 1024), dtype=np.int32)
        ac, pcl = [], len(self.pc)

        for fi in range(max_frames):
            if fi < pcl:
                tok = int(self.pc[fi, 0])
            elif fi == pcl:
                tok = self.audio_start
            else:
                tok = int(ac[-1][0])
            r = np.full((1, 1, 17), self.audio_pad, dtype=np.int32)
            r[0, 0, 0] = np.int32(tok)
            feed = {"input_ids": r, "past_valid_lengths": np.array([sl], dtype=np.int32)}
            for i in range(self.global_layers):
                feed[f"past_key_{i}"] = kv[f"k{i}"]
                feed[f"past_value_{i}"] = kv[f"v{i}"]
            o = self.sess_decode.run(None, feed)
            for i in range(self.global_layers):
                kv[f"k{i}"] = o[1 + i * 2]
                kv[f"v{i}"] = o[2 + i * 2]
            h, sl = o[0], sl + 1

            o2 = self.sess_fixed.run(None, {
                "global_hidden": h[:, -1, :].astype(np.float32),
                "repetition_seen_mask": np.expand_dims(rm.astype(np.int32), 0),
                "assistant_random_u": np.array([np.random.random()], dtype=np.float32),
                "audio_random_u": np.array([np.random.random(16)], dtype=np.float32),
            })
            ft = o2[1][0]
            if fi >= pcl:
                ac.append(ft.copy())
            for ci in range(16):
                t2 = int(ft[ci])
                if t2 < 1024:
                    rm[ci, t2] = min(rm[ci, t2] + 1, 5)
            if fi >= pcl and (not bool(o2[0][0, 0]) or fi >= max_frames - 1):
                break

        if not ac:
            return np.zeros((self.codec_channels, 0), dtype=np.float32), self.codec_sr, time.time() - t0

        codes = np.expand_dims(np.array(ac, dtype=np.int32), 0)
        o = self.sess_codec.run(None, {
            "audio_codes": codes,
            "audio_code_lengths": np.array([codes.shape[1]], dtype=np.int32),
        })
        alen = int(o[1][0]) if o[1].ndim == 1 else int(o[1][0, 0])
        return o[0][0, :, :alen], self.codec_sr, time.time() - t0
