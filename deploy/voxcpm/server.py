"""HTTP front for VoxCPM2 (Apache-2.0), CPU, float32.

Contract is fixed by tts-service/internal/backend/voxcpm.go:
  POST /synthesize {"text", "ref_wav_b64", "ref_text", "description"} -> audio/wav
    clone:  ref_wav_b64 + ref_text, no description
    design: description, no ref
  GET /healthz -> 200 once the model is resident. The model loads before the
  server accepts connections, so probes get connection-refused until then.
"""
import base64
import binascii
import hashlib
import io
import logging
import os
import tempfile
import threading
from contextlib import asynccontextmanager
from collections import OrderedDict
from typing import Optional

import soundfile as sf
from fastapi import FastAPI, HTTPException, Response
from pydantic import BaseModel

logging.basicConfig(level=logging.INFO)
log = logging.getLogger("voxcpm.server")

MAX_TEXT = 600
PROMPT_CACHE_SIZE = 8

_engine = None


class Engine:
    """One VoxCPM2 model, one inference at a time."""

    def __init__(self):
        import torch
        import voxcpm.model.voxcpm2 as vm
        from voxcpm import VoxCPM

        # The library keeps the checkpoint's bf16 on CPU; Zen 2 has no bf16
        # hardware, and bf16 measured ~3x slower than float32 on CPU.
        vm.pick_runtime_dtype = lambda device, configured: "float32"
        torch.set_num_threads(int(os.environ.get("TORCH_THREADS", "8")))
        self._m = VoxCPM.from_pretrained(
            "openbmb/VoxCPM2", load_denoiser=False, optimize=False, device="cpu")
        self.sample_rate = self._m.tts_model.sample_rate
        self._lock = threading.Lock()
        self._prompts = OrderedDict()

    def _prompt(self, ref_wav_path, ref_text, key):
        # build_prompt_cache/_generate_with_prompt_cache are private voxcpm
        # API (pinned 2.0.3): they let one encoded reference serve every
        # chunk of a break instead of re-encoding it per request.
        if key in self._prompts:
            self._prompts.move_to_end(key)
            return self._prompts[key]
        p = self._m.tts_model.build_prompt_cache(
            prompt_text=ref_text, prompt_wav_path=ref_wav_path, reference_wav_path=ref_wav_path)
        self._prompts[key] = p
        if len(self._prompts) > PROMPT_CACHE_SIZE:
            self._prompts.popitem(last=False)
        return p

    def render(self, text, ref_wav_path, ref_text, key=None):
        with self._lock:
            prompt = self._prompt(ref_wav_path, ref_text, key) if ref_wav_path else None
            gen = self._m.tts_model._generate_with_prompt_cache(
                target_text=text, prompt_cache=prompt, inference_timesteps=10,
                cfg_value=2.0, retry_badcase=True, streaming=False)
            try:
                wav, _, _ = next(gen)
            finally:
                gen.close()
            return wav.squeeze(0).cpu().numpy()


class SynthesizeRequest(BaseModel):
    text: str
    ref_wav_b64: Optional[str] = None
    ref_text: Optional[str] = None
    description: Optional[str] = None


@asynccontextmanager
async def lifespan(_app):
    global _engine
    if os.environ.get("VOXCPM_SKIP_LOAD") != "1":
        _engine = Engine()
        log.info("model resident, sample_rate=%d", _engine.sample_rate)
    yield


app = FastAPI(lifespan=lifespan)


@app.get("/healthz")
def healthz() -> Response:
    if _engine is None:
        return Response(status_code=503)
    return Response(status_code=200)


@app.post("/synthesize")
def synthesize(req: SynthesizeRequest) -> Response:
    if _engine is None:
        raise HTTPException(503, "model not loaded")
    text = " ".join(req.text.split())
    if not text:
        raise HTTPException(400, "text must not be empty")
    if len(text) > MAX_TEXT:
        raise HTTPException(400, f"text exceeds {MAX_TEXT} characters")
    clone = req.ref_wav_b64 is not None or req.ref_text is not None
    if clone and req.description is not None:
        raise HTTPException(400, "send either a reference or a description, not both")
    if clone and not (req.ref_wav_b64 and req.ref_text):
        raise HTTPException(400, "clone mode needs both ref_wav_b64 and ref_text")
    if not clone and not req.description:
        raise HTTPException(400, "send a reference or a description")

    if not clone:
        audio = _engine.render(f"({req.description}){text}", None, None)
    else:
        try:
            ref = base64.b64decode(req.ref_wav_b64, validate=True)
        except (binascii.Error, ValueError) as e:
            raise HTTPException(400, "ref_wav_b64 is not base64") from e
        try:
            sf.info(io.BytesIO(ref))
        except Exception as e:
            raise HTTPException(400, "ref_wav_b64 is not audio") from e
        key = hashlib.sha256(ref + b"\0" + req.ref_text.encode()).hexdigest()
        with tempfile.NamedTemporaryFile(suffix=".wav") as f:
            f.write(ref)
            f.flush()
            audio = _engine.render(text, f.name, req.ref_text, key)

    buf = io.BytesIO()
    sf.write(buf, audio, _engine.sample_rate, format="WAV")
    return Response(content=buf.getvalue(), media_type="audio/wav")
