"""HTTP front for VieNeu-TTS v3 Turbo (Apache-2.0, ONNX, CPU, torch-free).

Contract is fixed by tts-service/internal/backend/vieneu.go:
  POST /synthesize  {"text": str, "voice": str, "speed": float} -> audio/wav
  GET  /healthz     -> 200 once the model is resident

The model (int8 ONNX backbone + MOSS codec) is baked into the image at build
time via the `vieneu` package's own HuggingFace Hub cache — see Dockerfile.
HF_HUB_OFFLINE=1 at runtime means Vieneu() never touches the network; the
pod needs no persistent volume and no runtime download.
"""
import io
import logging
import threading
from typing import Optional

import soundfile as sf
import soxr
from fastapi import FastAPI, HTTPException, Response
from fastapi.responses import PlainTextResponse
from pydantic import BaseModel, Field

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger("vieneu.server")

app = FastAPI()

_model = None
_lock = threading.Lock()  # the ONNX engine is not proven safe under concurrent infer()


class SynthesizeRequest(BaseModel):
    # max_length matches the Go caller's own cap (internal/ttsserver/server.go),
    # itself derived from the default 4MB gRPC message limit -- this endpoint
    # is reachable directly inside the cluster and must not depend on a
    # well-behaved caller to avoid an oversized synthesis request serializing
    # every other request behind the global lock below.
    text: str = Field(..., max_length=2000)
    voice: Optional[str] = None
    speed: float = 1.0


@app.on_event("startup")
def load_model() -> None:
    global _model
    from vieneu import Vieneu

    logger.info("loading VieNeu-TTS v3 Turbo (int8, CPU)...")
    _model = Vieneu()
    logger.info("model resident, %d preset voices", len(_model.list_preset_voices()))


@app.get("/healthz")
def healthz() -> Response:
    if _model is None:
        return PlainTextResponse("model not loaded", status_code=503)
    return Response(content="ok", media_type="text/plain")


@app.post("/synthesize")
def synthesize(req: SynthesizeRequest) -> Response:
    if _model is None:
        raise HTTPException(status_code=503, detail="model not loaded")
    if not req.text.strip():
        raise HTTPException(status_code=400, detail="text must not be empty")

    sr = _model.sample_rate
    speed = req.speed if req.speed and req.speed > 0 else 1.0

    try:
        with _lock:
            audio = _model.infer(req.text, voice=req.voice or None)
    except ValueError as e:
        # Unknown voice name -- vieneu raises ValueError listing the available ones.
        raise HTTPException(status_code=400, detail=str(e)) from e

    if speed != 1.0:
        # Cheap playback-rate change: resample to sr/speed then declare the
        # original rate, so duration scales by 1/speed. Pitch shifts along
        # with speed -- acceptable for a DJ-style rate tweak, not a
        # pitch-preserving time-stretch.
        audio = soxr.resample(audio, sr, sr / speed)

    buf = io.BytesIO()
    sf.write(buf, audio, sr, format="WAV")
    return Response(content=buf.getvalue(), media_type="audio/wav")
