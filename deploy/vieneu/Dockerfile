# syntax=docker/dockerfile:1
#
# VieNeu-TTS v3 Turbo model server (Apache-2.0). Torch-free: onnxruntime runs
# the int8 backbone on CPU. Do NOT add a torch/[legacy]/[gpu] extra here --
# that reintroduces the dependency this image exists to avoid.
FROM python:3.12-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# Same non-root convention as the other images in this repo (tts-service's
# own Dockerfile, radio-service): fixed UID so it also works if a k8s
# securityContext pins runAsUser separately.
RUN useradd -u 10001 -m app

# Pin the top-level installs; vieneu's own deps (onnxruntime, numpy, soxr,
# soundfile, tokenizers, sea-g2p, perth, huggingface_hub) are transitive and
# torch-free by default -- confirmed via `pip list` (see deploy/vieneu/README.md).
RUN pip install --no-cache-dir \
      vieneu==3.2.3 \
      fastapi==0.141.1 \
      uvicorn==0.52.0

# Bake the model into the image: HF_HOME is populated once at build time
# (network required only here) and HF_HUB_OFFLINE=1 at runtime means the pod
# never calls out -- no persistent volume, no cold-start download.
#
# `Vieneu()` defaults to v3 Turbo, int8 backbone, CPU/ONNX. It fetches only
# the onnx_int8 subfolder of pnnbao-ump/VieNeu-TTS-v3-Turbo (not the 7.6GB
# full repo) plus the small OpenMOSS audio-codec repo -- about 285MB total.
# pin_model.py pins and enforces both repos' commits explicitly (fails the
# build if upstream `main` has drifted from the licence-reviewed revision)
# before letting `vieneu` do its own (unpinned) fetch against that same,
# now-verified cache -- see deploy/vieneu/README.md.
#
# Own the (still-empty) cache dir before downloading into it, then download
# as the non-root `app` user directly: a `chown -R` *after* populating a
# ~300MB tree would copy-up the whole tree into a new overlay layer and
# double the image size, since the root-owned layer underneath still exists.
ENV HF_HOME=/models/hf-cache
RUN mkdir -p /models/hf-cache && chown app:app /models/hf-cache
COPY --chown=app:app pin_model.py /tmp/pin_model.py
USER app
RUN python /tmp/pin_model.py && rm /tmp/pin_model.py
ENV HF_HUB_OFFLINE=1

COPY --chown=app:app server.py /app/server.py
WORKDIR /app
EXPOSE 8080
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8080"]
