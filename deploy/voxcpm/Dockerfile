# syntax=docker/dockerfile:1
#
# VoxCPM2 model server (Apache-2.0), CPU-only torch, float32 at runtime.
FROM python:3.12-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates ffmpeg libsndfile1 \
 && rm -rf /var/lib/apt/lists/*
RUN useradd -u 10001 -m app

COPY requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt && rm /tmp/requirements.txt

# Model baked in at build time; HF_HUB_OFFLINE=1 at runtime, so the pod does
# no network I/O. Cache dir is owned before download so no chown copy-up
# doubles the ~5GB layer.
ENV HF_HOME=/models/hf-cache
RUN mkdir -p /models/hf-cache && chown app:app /models/hf-cache
COPY --chown=app:app pin_model.py /tmp/pin_model.py
USER app
RUN python /tmp/pin_model.py && rm /tmp/pin_model.py
ENV HF_HUB_OFFLINE=1

COPY --chown=app:app server.py test_server.py /app/
WORKDIR /app
EXPOSE 8080
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8080", "--workers", "1"]
