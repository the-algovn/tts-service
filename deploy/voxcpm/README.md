# VoxCPM2 model server

Self-hosted multilingual TTS backend for `tts-service` (CPU, float32).
Implements the HTTP contract fixed by `internal/backend/voxcpm.go`:

```
POST /synthesize {"text", "ref_wav_b64", "ref_text", "description"} -> audio/wav
  clone:  ref_wav_b64 + ref_text, no description
  design: description, no ref
GET /healthz -> 200 once the model is resident. The model loads before the
  server accepts connections, so probes get connection-refused until then.
```

Unused fields are sent as JSON null. Errors are non-200 with a short body:
400 for bad requests, 503 when the model is not loaded or another render
holds the engine (one render at a time per pod). Text is limited to 600
characters.

A render stops at the next audio patch once its client disconnects (the
server answers 499 to nobody), so a request tts-service gave up on does not
hold the pod for minutes.

Environment: `TORCH_THREADS` (default 8) and `VOXCPM_TIMESTEPS`, the diffusion
steps per audio patch (default 10; fewer is faster and rougher).

## Model and licence

- Package: [`voxcpm`](https://pypi.org/project/voxcpm/) `2.0.3`, Apache-2.0.
- Model: `openbmb/VoxCPM2`, Apache-2.0. Revision pinned and enforced:
  **`32279effe8c19989596f05d353d1447f51d9e915`**. `pin_model.py` runs at
  build time, fails the build if `main` has drifted from the pin, then
  downloads exactly that revision. The runtime is offline (`HF_HUB_OFFLINE=1`).

## Why float32

The library keeps the checkpoint's bf16 on CPU. The target nodes (Zen 2) have
no bf16 hardware and bf16 measured about 3x slower than float32, so
`server.py` forces float32 by patching `pick_runtime_dtype`.

## Prompt cache and private API

Clone requests reuse an encoded reference through an 8-entry LRU keyed by
sha256(ref audio + ref_text), via `build_prompt_cache` and
`_generate_with_prompt_cache`. These are private voxcpm APIs, which is why
`voxcpm` is pinned to `2.0.3`; re-verify them against `voxcpm/core.py` before
bumping.

## Dependencies

`requirements.txt` is a linux x86_64 lock compiled from `requirements.in`:

```bash
uv pip compile requirements.in --python-version 3.12 \
  --python-platform x86_64-manylinux_2_28 --index-strategy unsafe-best-match \
  --emit-index-url -o requirements.txt
```

torch is `2.14.1+cpu`; torchaudio is `2.11.0+cpu` (newest CPU build on the
PyTorch index, 2.14.1 does not exist there).

## Build, test, push

The image is pushed by hand not by CI.

```bash
cd deploy/voxcpm
docker buildx build --platform linux/amd64 -t ghcr.io/the-algovn/tts-service-voxcpm:main --load .
docker run --rm --network none -e VOXCPM_SKIP_LOAD=1 ghcr.io/the-algovn/tts-service-voxcpm:main pytest -q test_server.py
docker run -d --name vx -p 8080:8080 -e TORCH_THREADS=8 ghcr.io/the-algovn/tts-service-voxcpm:main
until curl -sf localhost:8080/healthz; do sleep 5; done
curl -s -X POST localhost:8080/synthesize -H 'Content-Type: application/json' \
  -d '{"text":"Chao buoi toi ca nha, day la AlgoVN Radio!","description":"giong nu tre, tuoi vui"}' -o /tmp/vx.wav
ffprobe -v error -show_entries format=duration /tmp/vx.wav
docker rm -f vx
docker push ghcr.io/the-algovn/tts-service-voxcpm:main
```

Local tests without the image: `VOXCPM_SKIP_LOAD=1 pytest -q test_server.py`.
