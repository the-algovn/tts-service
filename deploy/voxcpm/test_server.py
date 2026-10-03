import base64
import io

import numpy as np
import pytest
import soundfile as sf
from fastapi.testclient import TestClient

import server


class FakeModel:
    sample_rate = 48000

    def __init__(self):
        self.calls = []

    def render(self, text, ref_wav_path, ref_text, key=None):
        self.calls.append((text, ref_wav_path, ref_text))
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
