# ── YOLO detection service ─────────────────────────────────────
# Event-triggered object + PPE detection. Called on-demand by the
# Go API over HTTP (port 8501). Runs on GPU; falls back to CPU if
# CUDA isn't visible to the container (useful for dev, unusable
# for prod — a single inference on CPU is ~2s vs ~30ms on a 3070).
#
# Base image: pytorch-runtime already ships with torch + cuDNN
# compiled against CUDA 12.1, shaving ~5 minutes off `pip install
# torch` per build. ~5GB compressed — acceptable tax for AI work.
FROM docker.io/pytorch/pytorch:2.3.0-cuda12.1-cudnn8-runtime

# libGL is pulled by OpenCV (via ultralytics). libglib2.0-0 covers
# the symbol-lookup error you get otherwise. curl is for healthcheck.
RUN apt-get update && apt-get install -y --no-install-recommends \
        libgl1 \
        libglib2.0-0 \
        curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Install deps first so source edits don't bust the pip cache layer.
# torch is already in the base image; ultralytics pulls opencv+more.
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

# Copy service code and bundled model weights. The .dockerignore next
# to this file keeps __pycache__ and logs out of the image.
COPY . .

# Non-root runtime — matches the api/worker image convention.
RUN groupadd --system --gid 10003 ironsight && \
    useradd  --system --uid 10003 --gid ironsight --home /app ironsight && \
    chown -R ironsight:ironsight /app
USER ironsight

ENV PORT=8501 \
    YOLO_MODEL=yolo11n.pt \
    PPE_MODEL=models/ppe.pt \
    PYTHONUNBUFFERED=1

EXPOSE 8501

HEALTHCHECK --interval=30s --timeout=5s --start-period=45s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8501/health || exit 1

CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8501"]
