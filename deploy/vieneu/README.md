# VieNeu-TTS v3 Turbo model server

Self-hosted Vietnamese TTS backend for `tts-service`. Implements the HTTP
contract fixed by `internal/backend/vieneu.go`:

- `POST /synthesize` `{"text": str, "voice": str, "speed": float}` -> raw
  `audio/wav` bytes, `Content-Type: audio/wav`
- `GET /healthz` -> `200` once the model is resident (`503` before)

## Model and licence

- SDK: [`vieneu`](https://pypi.org/project/vieneu/) `3.2.3` (PyPI), Apache-2.0.
- Model: `pnnbao-ump/VieNeu-TTS-v3-Turbo`, Apache-2.0, tags `vi`+`en`.
  Revision baked into this image: **`75ff82a72f54d55ed389e1eeb12041d3c4bac7d4`**
  (the `vieneu` SDK does not expose a `revision=` pin -- it always resolves
  `main`; this is the commit `main` resolved to at build time, confirmed by
  the HF Hub API and by the snapshot directory actually cached in the image).
- Secondary dependency: `OpenMOSS-Team/MOSS-Audio-Tokenizer-Nano-ONNX`
  (the MOSS audio codec used by the ONNX engine), Apache-2.0, resolved
  commit `ceff0d0749bfb3fa2d61149794ec6feef0d1e1ae`.
- Precision: **int8** backbone (the SDK's CPU default -- `onnx_int8`
  subfolder), not fp32.

**Non-commercial artifact avoided.** The pip package also bundles an older,
6-voice preset file (`vieneu/assets/voices.json`) for the base 0.3B/GGUF
model, explicitly marked `"license": "CC BY-NC 4.0"` /
`"non-commercial use only"` in its own metadata. That file and the GGUF
model it belongs to are **not used**: the default `Vieneu()` call (mode
`v3turbo`) loads voices from a *different*, unrestricted bundled file,
`vieneu/assets/voices_v3_turbo.json` ("v3 turbo curated preset voices"),
which carries no such restriction and ships under the package's Apache-2.0
licence. The GGUF path (`mode="standard"`) is never invoked by this server.

## Voice identifiers

Confirmed by calling the installed SDK's `Vieneu().list_preset_voices()`
inside the built image (14 voices, source:
`vieneu/assets/voices_v3_turbo.json`, default `Phạm Tuyên`):

| Voice ID (pass as `"voice"`) | Gender | Region (miền) | Style |
|---|---|---|---|
| Minh Đức | male | Bắc | tin_tuc (news) |
| Phạm Tuyên *(default)* | male | Bắc | tu_nhien (natural) |
| Thái Sơn | male | Nam | doc_truyen (storytelling) |
| Xuân Vĩnh | male | Nam | tu_nhien |
| Thanh Bình | male | Bắc | doc_truyen |
| Trúc Ly | female | Bắc | tu_nhien |
| Ngọc Linh | female | Bắc | doc_truyen |
| Đoan Trang | female | Bắc | tu_nhien |
| Mai Anh | female | Bắc | tin_tuc |
| Thục Đoan | female | Nam | doc_truyen |
| Minh Triết | male | Nam | tin_tuc |
| Thùy Dung | female | Nam | tin_tuc |
| Quang Sơn | male | Trung | tu_nhien |
| Ngọc Trân | female | Trung | tu_nhien |

These are curated built-in voices (speaker embedding + reference codes),
no reference clip required. They are distinct from -- and must not be
confused with -- the 6 voices in the CC-BY-NC base `voices.json`
(`Vinh`/`Binh`/`Tuyen`/`Doan`/`Ly`/`Ngoc`).

## Speed handling

The `vieneu` SDK's `infer()` has no rate/speed parameter. `speed` is
implemented server-side as a cheap playback-rate change: the waveform is
resampled from `sr` to `sr/speed` (via `soxr`, already a `vieneu` dependency)
and re-declared at the original rate, so duration scales by `1/speed`.
Pitch shifts along with speed -- this is not a pitch-preserving time-stretch.
`speed=1.0` (or omitted/non-positive) skips resampling entirely.

## Build

```bash
podman build -t vieneu-tts:dev -f deploy/vieneu/Dockerfile deploy/vieneu/
```

Model download happens **only at build time** (`HF_HOME=/models/hf-cache`
populated by one `Vieneu()` call), then `HF_HUB_OFFLINE=1` is set for
runtime -- the pod does no network I/O and needs no persistent volume.
Only the `onnx_int8` subfolder of the 7.6GB v3-Turbo repo is fetched
(~165MB) plus the ~90MB MOSS codec repo: **~285MB of model weights**, not
the full repo.

- **Final image size: 895 MB** (`podman images`). Most of this is Python
  deps, not the model: `onnxruntime` (~200MB installed) and `gradio`
  (~pandas/pillow/starlette, a *required* -- not optional -- transitive
  dependency of `vieneu==3.2.3`, pulled in even though this server never
  imports it) account for more of the image than the ~285MB of model
  weights do.
- **Architecture note:** built and tested as `linux/arm64` because that is
  what this development machine's Podman VM runs natively. The target
  cluster (single Proxmox host, per the project's other Dockerfiles) is
  `linux/amd64` -- **rebuild with `--platform linux/amd64` before deploying**
  (Task 10). CPU inference under amd64 (native, not emulated, on the actual
  cluster hardware) has not been measured here.

Pinned top-level installs: `vieneu==3.2.3`, `fastapi==0.141.1`,
`uvicorn==0.52.0`. Transitive deps (torch-free, confirmed by `pip list`
inside the image -- no `torch`/`torchaudio`/`transformers`/`llama-cpp-python`
present):

```
onnxruntime==1.28.0  numpy==2.5.1  soundfile==0.14.0  soxr==1.1.0
tokenizers==0.23.1  sea-g2p==0.7.20  perth==1.0.0 (MIT)
huggingface_hub==1.26.0  gradio==6.22.0  pandas==3.0.5  pillow==12.3.0  ...
```

## Smoke test (2026-07-31, `linux/arm64`, Podman VM: 6 vCPU / 6GB RAM)

Ran with `podman run -d -p 18080:8080 vieneu-tts:dev`, then:

```bash
curl -s -X POST localhost:18080/synthesize \
  -H 'Content-Type: application/json' \
  -d '{"text":"Xin chào các bạn, đây là Tần Số bốn hai, và bây giờ là một bản nhạc mới.","voice":"Phạm Tuyên","speed":1.0}' \
  -o vieneu-sample.wav
```

| Sample | Voice | Wall clock | Audio duration | RTF |
|---|---|---|---|---|
| `vieneu-sample.wav` | Phạm Tuyên (male, Bắc, default) | 0.607 s | 3.60-3.76 s | ~0.16-0.17 |
| `vieneu-sample-male.wav` | Phạm Tuyên (male, Bắc) | 0.610 s | 3.92 s | ~0.16 |
| `vieneu-sample-female.wav` | Trúc Ly (female, Bắc) | 0.600 s | 4.40 s | ~0.14 |

All three: `200 OK`, `RIFF ... WAVE ... Microsoft PCM, 16 bit, mono 48000 Hz`
(confirmed with `file`/`ffprobe`). RTF well under 1 on CPU, consistent with
the SDK's claimed CPU performance.

**Podman VM memory matters.** The default Podman machine on this host was
provisioned with only 2048MB RAM, which OOM-killed the container (exit 137)
on the first `/synthesize` call with no error logged. Bumped to 6144MB
(`podman machine set --memory 6144`) and the container ran the same
requests without issue. This is a real constraint for the target cluster
node, not just a dev-machine quirk.

**Container RSS:** ~654 MB immediately after model load (before any
request); ~890 MB-1.2 GB after several `/synthesize` calls (`podman stats`).
Budget at least ~1.5 GB per pod for headroom.

**Error handling** verified: an unknown voice name returns `400` with a
message listing all 14 valid ids (`"Voice 'X' not found. Available: [...]"`).

## Not done here (out of scope for this build/smoke-test step)

- Listening-quality judgement against `vi-VN-Wavenet-B` (tone accuracy) --
  human gate, sequenced separately.
- Voice catalog entries in `internal/catalog/catalog.go`.
- `linux/amd64` build for the actual cluster.
