# ── Qwen VLM reasoning / indexer service ───────────────────────
# Vision-language model for scene description and semantic search
# indexing. The worker container calls /analyze_video?mode=describe
# against this service per recorded segment to build the search
# corpus. HTTP on port 8502.
#
# Same base as the YOLO service — PyTorch CUDA runtime. Transformers
# downloads model weights from HuggingFace on first run (no bundled
# weights); mount /root/.cache/huggingface from a named volume so the
# ~5GB download survives container restarts.
# Pairing notes: this base ships torch 2.3.0 + CUDA 12.1. transformers
# >= 4.55 started requiring torch >= 2.4 (it silently disables PyTorch
# otherwise and falls back to config/tokenizer-only mode — your app
# won't error, it'll just run with mock responses, which is a miserable
# failure mode to diagnose). requirements.txt pins transformers < 4.55
# so the two stay compatible. When bumping the base to pytorch 2.5.x or
# newer you can lift that ceiling.
FROM docker.io/pytorch/pytorch:2.3.0-cuda12.1-cudnn8-runtime

# ffmpeg + decord deps — Qwen video mode decodes segment clips. We
# force the decord backend in server.py (torchvision/PyAV produces
# 0 frames for our fMP4 fragments on some hosts).
RUN apt-get update && apt-get install -y --no-install-recommends \
        ffmpeg \
        libgl1 \
        libglib2.0-0 \
        curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY requirements.txt ./
# decord isn't always in PyPI as a wheel for all arch/python combos;
# install it separately so a miss here is an obvious error rather
# than a confusing runtime ImportError.
RUN pip install --no-cache-dir -r requirements.txt && \
    pip install --no-cache-dir decord

COPY . .

RUN groupadd --system --gid 10004 ironsight && \
    useradd  --system --uid 10004 --gid ironsight --home /app ironsight && \
    mkdir -p /home/ironsight/.cache/huggingface && \
    chown -R ironsight:ironsight /app /home/ironsight
USER ironsight

ENV PORT=8502 \
    QWEN_MODEL=Qwen/Qwen2.5-VL-3B-Instruct \
    HF_HOME=/home/ironsight/.cache/huggingface \
    PYTHONUNBUFFERED=1 \
    FORCE_QWENVL_VIDEO_READER=decord

EXPOSE 8502

# Longer start-period than YOLO — first run downloads ~5GB of
# weights from HuggingFace. After warm cache, startup is ~30s.
HEALTHCHECK --interval=30s --timeout=5s --start-period=600s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8502/health || exit 1

CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8502"]
