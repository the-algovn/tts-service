# syntax=docker/dockerfile:1
#
# VieNeu-TTS v3 Turbo model server (Apache-2.0). Torch-free: onnxruntime runs
# the int8 backbone on CPU. Do NOT add a torch/[legacy]/[gpu] extra here --
# that reintroduces the dependency this image exists to avoid.
FROM python:3.12-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/*

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
# Both repos resolved to Apache-2.0 revisions at build time (see README).
ENV HF_HOME=/models/hf-cache
RUN python -c "from vieneu import Vieneu; Vieneu()"
ENV HF_HUB_OFFLINE=1

COPY server.py /app/server.py
WORKDIR /app
EXPOSE 8080
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8080"]
