import asyncio
import base64
import io
import re
import threading
from collections import OrderedDict
from types import SimpleNamespace

import numpy as np
import pytest
import soundfile as sf
from fastapi.testclient import TestClient

import server


class FakeModel:
    sample_rate = 48000

    def __init__(self):
        self.calls = []
        self.keys = []

    def render(self, text, ref_wav_path, ref_text, key=None, cancel=None):
        self.calls.append((text, ref_wav_path, ref_text))
        self.keys.append(key)
        return np.zeros(4800, dtype=np.float32)


@pytest.fixture
def client(monkeypatch):
    fake = FakeModel()
    monkeypatch.setattr(server, "_engine", fake)
    return TestClient(server.app), fake


def wav_b64():
    buf = io.BytesIO()
    sf.write(buf, np.zeros(48000, dtype=np.float32), 48000, format="WAV")
    return base64.b64encode(buf.getvalue()).decode()


def test_healthz_503_until_loaded(monkeypatch):
    monkeypatch.setattr(server, "_engine", None)
    assert TestClient(server.app).get("/healthz").status_code == 503


def test_clone_mode(client):
    c, fake = client
    r = c.post("/synthesize", json={"text": "xin chao", "ref_wav_b64": wav_b64(), "ref_text": "chao"})
    assert r.status_code == 200
    assert r.headers["content-type"] == "audio/wav"
    text, path, ref_text = fake.calls[0]
    assert text == "xin chao" and path is not None and ref_text == "chao"
    assert re.fullmatch(r"[0-9a-f]{64}", fake.keys[0])


def test_design_mode_prefixes_description(client):
    c, fake = client
    r = c.post("/synthesize", json={"text": "xin chao", "description": "giong nu tre"})
    assert r.status_code == 200
    assert fake.calls[0] == ("(giong nu tre)xin chao", None, None)


@pytest.mark.parametrize("body", [
    {"text": ""},
    {"text": "x"},
    {"text": "x", "ref_wav_b64": wav_b64()},
    {"text": "x", "ref_wav_b64": wav_b64(), "ref_text": "a", "description": "d"},
    {"text": "x" * 601, "description": "d"},
    {"text": "x", "ref_wav_b64": "!!notbase64", "ref_text": "a"},
])
def test_bad_requests(client, body):
    c, _ = client
    assert c.post("/synthesize", json=body).status_code == 400


def test_non_audio_ref_is_400(client):
    c, _ = client
    ref = base64.b64encode(b"definitely not a wav file").decode()
    r = c.post("/synthesize", json={"text": "x", "ref_wav_b64": ref, "ref_text": "a"})
    assert r.status_code == 400


def test_prompt_lru():
    built = []

    def build(prompt_text, prompt_wav_path, reference_wav_path):
        built.append(prompt_text)
        return object()

    eng = object.__new__(server.Engine)
    eng._m = SimpleNamespace(tts_model=SimpleNamespace(build_prompt_cache=build))
    eng._prompts = OrderedDict()

    first = eng._prompt("p", "k0", "k0")
    assert eng._prompt("p", "k0", "k0") is first
    assert len(built) == 1
    for i in range(1, server.PROMPT_CACHE_SIZE):
        eng._prompt("p", f"k{i}", f"k{i}")
    eng._prompt("p", "k0", "k0")
    assert list(eng._prompts)[-1] == "k0"
    eng._prompt("p", "k8", "k8")
    assert len(eng._prompts) == server.PROMPT_CACHE_SIZE
    assert "k1" not in eng._prompts and "k0" in eng._prompts


def test_busy_engine_is_503(client):
    c, fake = client

    def busy(*_a, **_k):
        raise server.Busy()

    fake.render = busy
    r = c.post("/synthesize", json={"text": "xin chao", "description": "giong nu"})
    assert r.status_code == 503
    assert r.json()["detail"] == "busy"


def test_render_rejects_while_lock_held():
    eng = object.__new__(server.Engine)
    eng._lock = threading.Lock()
    with eng._lock:
        with pytest.raises(server.Busy):
            eng.render("x", None, None)


def test_cancel_hook_raises_only_once_cancelled():
    eng = object.__new__(server.Engine)
    eng._cancel = None
    eng._check_cancel(None, ())
    eng._cancel = threading.Event()
    eng._check_cancel(None, ())
    eng._cancel.set()
    with pytest.raises(server.Cancelled):
        eng._check_cancel(None, ())


def test_client_disconnect_cancels_render(monkeypatch):
    started = threading.Event()

    class Blocking(FakeModel):
        def render(self, text, ref_wav_path, ref_text, key=None, cancel=None):
            started.set()
            assert cancel.wait(5)
            raise server.Cancelled()

    class GoneClient:
        async def is_disconnected(self):
            return started.is_set()

    monkeypatch.setattr(server, "_engine", Blocking())
    monkeypatch.setattr(server, "DISCONNECT_POLL", 0.01)
    req = server.SynthesizeRequest(text="xin chao", description="giong nu")
    assert asyncio.run(server.synthesize(req, GoneClient())).status_code == 499
