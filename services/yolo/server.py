"""YOLO Detection Service for Ironsight VMS.

Event-triggered: the Go server sends a snapshot JPEG when camera metadata
fires an analytics event. Runs TWO models:

  1. Security model (person/vehicle/etc from COCO)
  2. PPE model (hardhat, safety vest, mask — optional)

Both models run on the GPU in parallel and return combined results.

Usage:
    uvicorn server:app --host 0.0.0.0 --port 8501
    YOLO_MODEL=yolo11m.pt YOLO_DEVICE=cuda:1 PPE_MODEL=models/ppe.pt \
        uvicorn server:app --host 0.0.0.0 --port 8501
"""

import io
import os
import time
import logging

import numpy as np
import torch
from PIL import Image
from fastapi import FastAPI, File, UploadFile
from fastapi.responses import JSONResponse
from ultralytics import YOLO

logging.basicConfig(level=logging.INFO, format="%(asctime)s [YOLO] %(message)s")
logger = logging.getLogger(__name__)

# ── Configuration ──
MODEL_NAME = os.getenv("YOLO_MODEL", "yolo11n.pt")
PPE_MODEL_PATH = os.getenv("PPE_MODEL", "models/ppe.pt")
CONFIDENCE_THRESHOLD = float(os.getenv("YOLO_CONF", "0.25"))
PPE_CONFIDENCE = float(os.getenv("PPE_CONF", "0.35"))

# Device selection: prefer CUDA device specified by YOLO_DEVICE, else cuda:0, else cpu
_requested_device = os.getenv("YOLO_DEVICE")
if _requested_device:
    DEVICE = _requested_device
elif torch.cuda.is_available():
    DEVICE = "cuda:0"
else:
    DEVICE = "cpu"

# Security-relevant COCO classes — filter out furniture, food, etc.
SECURITY_CLASSES = {
    "person", "bicycle", "car", "motorcycle", "bus", "truck",
    "dog", "cat", "bird", "horse", "bear",
    "backpack", "umbrella", "handbag", "suitcase",
    "knife", "scissors", "baseball bat",
    "cell phone", "laptop",
}

# PPE violation classes (missing PPE — the SOC should flag these)
PPE_VIOLATION_CLASSES = {
    "nohat", "no-hat", "no_hat",
    "no-hardhat", "no_hardhat", "no hardhat", "no-helmet", "no_helmet",
    "novest", "no-vest", "no_vest",
    "no-safety-vest", "no_safety_vest", "no safety vest",
    "no-mask", "no_mask", "no mask",
    "no-glove", "no_glove", "no_gloves",
    "no-goggles", "no_goggles",
    "no-shoes", "no_shoes",
}
PPE_COMPLIANCE_CLASSES = {
    "hat", "hardhat", "helmet", "hard-hat", "hard_hat",
    "vest", "safety-vest", "safety_vest", "hi-vis",
    "mask", "face-mask", "face_mask",
    "gloves", "glove",
    "goggles",
    "shoes", "safety_shoe",
}
# Human-readable labels for SOC display
PPE_VIOLATION_LABELS = {
    "nohat": "Hard Hat", "no-hat": "Hard Hat", "no_hat": "Hard Hat",
    "no-hardhat": "Hard Hat", "no_hardhat": "Hard Hat", "no-helmet": "Helmet",
    "novest": "Hi-Vis Vest", "no-vest": "Hi-Vis Vest", "no_vest": "Hi-Vis Vest",
    "no-safety-vest": "Safety Vest", "no_safety_vest": "Safety Vest",
    "no-mask": "Face Mask", "no_mask": "Face Mask",
    "no-glove": "Gloves", "no_gloves": "Gloves",
    "no-goggles": "Goggles", "no_goggles": "Goggles",
    "no-shoes": "Safety Shoes", "no_shoes": "Safety Shoes",
}

# ── Log GPU info ──
if torch.cuda.is_available():
    for i in range(torch.cuda.device_count()):
        name = torch.cuda.get_device_name(i)
        mem = torch.cuda.get_device_properties(i).total_memory / (1024**3)
        logger.info(f"GPU {i}: {name} ({mem:.1f}GB)")
    logger.info(f"Using device: {DEVICE}")
else:
    logger.info("Running on CPU (no CUDA available)")

# ── Load security model ──
logger.info(f"Loading security model {MODEL_NAME} on {DEVICE}...")
start = time.time()
model = YOLO(MODEL_NAME)
model.to(DEVICE)
logger.info(f"Security model loaded in {(time.time() - start) * 1000:.0f}ms")

# ── Load PPE model (optional) ──
ppe_model = None
if os.path.exists(PPE_MODEL_PATH):
    try:
        logger.info(f"Loading PPE model {PPE_MODEL_PATH} on {DEVICE}...")
        start = time.time()
        ppe_model = YOLO(PPE_MODEL_PATH)
        ppe_model.to(DEVICE)
        logger.info(f"PPE model loaded in {(time.time() - start) * 1000:.0f}ms")
        logger.info(f"PPE classes: {list(ppe_model.names.values())}")
    except Exception as e:
        logger.warning(f"Failed to load PPE model: {e}")
        ppe_model = None
else:
    logger.info(f"No PPE model at {PPE_MODEL_PATH} — PPE detection disabled")

# ── Warm up ──
logger.info("Warming up models...")
dummy = np.zeros((640, 640, 3), dtype=np.uint8)
model.predict(dummy, verbose=False, conf=CONFIDENCE_THRESHOLD)
if ppe_model:
    ppe_model.predict(dummy, verbose=False, conf=PPE_CONFIDENCE)
logger.info("Warm-up complete — ready for inference")

app = FastAPI(title="Ironsight YOLO Detection Service")


# pynvml is the canonical NVIDIA Management Library binding. It works
# wherever the WSL CUDA driver is exposed and gives us live GPU
# utilization, VRAM, and temperature — none of which torch.cuda exposes
# directly. Soft import so the service still boots if the wheel is
# unavailable; in that case the GPU section of /health drops to None.
try:
    import pynvml  # type: ignore
    pynvml.nvmlInit()
    _NVML_OK = True
except Exception:
    _NVML_OK = False


def gpu_stats() -> dict:
    """Live GPU stats for the device this service is using. Returns a
    dict with stable keys; missing fields come back as None so the Go
    consumer doesn't have to special-case schema drift."""
    out = {
        "gpu_util_pct": None,
        "gpu_memory_used_mb": None,
        "gpu_memory_total_mb": None,
        "gpu_temperature_c": None,
    }
    if not torch.cuda.is_available():
        return out
    idx = int(DEVICE.split(":")[1]) if DEVICE.startswith("cuda:") else 0
    # torch fallback for memory — works without NVML.
    try:
        free, total = torch.cuda.mem_get_info(idx)
        out["gpu_memory_used_mb"] = round((total - free) / (1024 * 1024))
        out["gpu_memory_total_mb"] = round(total / (1024 * 1024))
    except Exception:
        pass
    if _NVML_OK:
        try:
            handle = pynvml.nvmlDeviceGetHandleByIndex(idx)
            util = pynvml.nvmlDeviceGetUtilizationRates(handle)
            out["gpu_util_pct"] = int(util.gpu)
            mem = pynvml.nvmlDeviceGetMemoryInfo(handle)
            out["gpu_memory_used_mb"] = round(mem.used / (1024 * 1024))
            out["gpu_memory_total_mb"] = round(mem.total / (1024 * 1024))
            out["gpu_temperature_c"] = pynvml.nvmlDeviceGetTemperature(handle, pynvml.NVML_TEMPERATURE_GPU)
        except Exception:
            # NVML calls fail intermittently when the driver is busy
            # transcoding or under contention. Fall back silently —
            # torch numbers are still populated above.
            pass
    return out


@app.get("/health")
async def health():
    return {
        "status": "ok",
        "model": MODEL_NAME,
        "ppe_model": PPE_MODEL_PATH if ppe_model else None,
        "ppe_enabled": ppe_model is not None,
        "device": DEVICE,
        "cuda_available": torch.cuda.is_available(),
        "gpu_name": torch.cuda.get_device_name(int(DEVICE.split(":")[1])) if DEVICE.startswith("cuda") else None,
        **gpu_stats(),
    }


def box_to_dict(box, img_w, img_h, cls_name):
    x1, y1, x2, y2 = box.xyxy[0].tolist()
    return {
        "class": cls_name,
        "confidence": round(float(box.conf[0]), 4),
        "bbox": {
            "x1": round(x1, 1),
            "y1": round(y1, 1),
            "x2": round(x2, 1),
            "y2": round(y2, 1),
        },
        "bbox_normalized": {
            "x1": round(x1 / img_w, 4),
            "y1": round(y1 / img_h, 4),
            "x2": round(x2 / img_w, 4),
            "y2": round(y2 / img_h, 4),
        },
    }


@app.post("/detect")
async def detect(image: UploadFile = File(...)):
    """Run security + PPE detection on a JPEG/PNG image."""
    start = time.time()

    image_bytes = await image.read()
    try:
        pil_image = Image.open(io.BytesIO(image_bytes)).convert("RGB")
    except Exception as e:
        return JSONResponse(status_code=400, content={"error": f"Invalid image: {e}"})

    img_w, img_h = pil_image.size

    # ── Run security model ──
    sec_start = time.time()
    sec_results = model.predict(
        pil_image, verbose=False, conf=CONFIDENCE_THRESHOLD, device=DEVICE,
    )
    sec_ms = (time.time() - sec_start) * 1000

    detections = []
    filtered_out = []
    has_person = False
    if sec_results and len(sec_results) > 0:
        result = sec_results[0]
        if result.boxes is not None:
            for box in result.boxes:
                cls_id = int(box.cls[0])
                cls_name = model.names.get(cls_id, f"class_{cls_id}")

                if cls_name not in SECURITY_CLASSES:
                    filtered_out.append(cls_name)
                    continue
                if cls_name == "person":
                    has_person = True

                detections.append(box_to_dict(box, img_w, img_h, cls_name))

    # ── Run PPE model (only if person detected and model loaded) ──
    ppe_detections = []
    ppe_violations = []
    ppe_ms = 0
    if ppe_model and has_person:
        ppe_start = time.time()
        ppe_results = ppe_model.predict(
            pil_image, verbose=False, conf=PPE_CONFIDENCE, device=DEVICE,
        )
        ppe_ms = (time.time() - ppe_start) * 1000

        if ppe_results and len(ppe_results) > 0:
            result = ppe_results[0]
            if result.boxes is not None:
                for box in result.boxes:
                    cls_id = int(box.cls[0])
                    cls_name = ppe_model.names.get(cls_id, f"class_{cls_id}").lower()

                    detection = box_to_dict(box, img_w, img_h, cls_name)
                    ppe_detections.append(detection)

                    # Flag violations (no-hardhat, no-mask, etc.)
                    if cls_name in PPE_VIOLATION_CLASSES:
                        # Attach human-readable label for SOC display
                        detection["missing"] = PPE_VIOLATION_LABELS.get(cls_name, cls_name)
                        ppe_violations.append(detection)

    total_ms = (time.time() - start) * 1000

    # Logging
    log_parts = [f"security={len(detections)} ({sec_ms:.0f}ms)"]
    if ppe_model:
        log_parts.append(f"PPE={len(ppe_detections)} ({ppe_ms:.0f}ms)")
        if ppe_violations:
            log_parts.append(f"VIOLATIONS: {', '.join(v['class'] for v in ppe_violations)}")
    if detections or ppe_detections:
        logger.info(
            f"{' | '.join(log_parts)} total={total_ms:.0f}ms: "
            + ", ".join(f"{d['class']}({d['confidence']:.0%})" for d in (detections + ppe_detections)[:8])
        )

    return {
        "detections": detections,
        "ppe_detections": ppe_detections,
        "ppe_violations": ppe_violations,
        "inference_ms": round(total_ms, 1),
        "security_ms": round(sec_ms, 1),
        "ppe_ms": round(ppe_ms, 1),
        "model": MODEL_NAME,
        "ppe_model": os.path.basename(PPE_MODEL_PATH) if ppe_model else None,
        "device": DEVICE,
        "image_size": {"width": img_w, "height": img_h},
    }
