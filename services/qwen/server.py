"""Qwen vLM Reasoning Service for Ironsight VMS.

Takes a surveillance frame + YOLO detections and produces a structured
security assessment: threat level, natural language description,
recommended action, and false positive likelihood.

Usage:
    uvicorn server:app --host 0.0.0.0 --port 8502
    QWEN_MODEL=Qwen/Qwen2.5-VL-72B-Instruct-AWQ uvicorn server:app --port 8502
"""

import io
import json
import os
import re
import tempfile
import time
import logging

import torch
from PIL import Image
from fastapi import FastAPI, File, Form, UploadFile
from fastapi.responses import JSONResponse

# Force qwen-vl-utils to use the decord backend for video decoding. The
# default torchvision/PyAV backend returns 0 frames on Windows for many of
# our FFmpeg-produced fMP4 clips (missing video_fps in info, unreadable
# fragment structure, etc). decord handles both correctly out of the box.
os.environ.setdefault("FORCE_QWENVL_VIDEO_READER", "decord")

logging.basicConfig(level=logging.INFO, format="%(asctime)s [QWEN] %(message)s")
logger = logging.getLogger(__name__)

# ── Configuration ──
MODEL_NAME = os.getenv("QWEN_MODEL", "Qwen/Qwen2.5-VL-3B-Instruct")
_requested_device = os.getenv("QWEN_DEVICE")
if _requested_device:
    DEVICE = _requested_device
elif torch.cuda.is_available():
    DEVICE = "cuda:0"
else:
    DEVICE = "cpu"
MIN_VRAM_GB = float(os.getenv("QWEN_MIN_VRAM_GB", "6"))  # 3B fits in 6-7GB at fp16

# Used by /analyze_video?mode=describe — the background indexer's prompt.
# Deliberately neutral and searchable: no threat assessment, no judgments.
# The output becomes the FTS/tags corpus for /api/search/semantic so word
# choice matters — prefer specific visible nouns and adjectives over vague
# language. Operators and customers will type exact words they expect to
# see here ("red truck", "backpack", "ladder", "climbing").
DESCRIBE_PROMPT = """You are a surveillance video indexer. Describe this short clip factually so it can be searched later by keyword. Do NOT assess threat. Use specific visible language — colors, clothing, objects carried, vehicle types, activities.

Respond with ONLY a JSON object:
{
  "description": "1-2 factual sentences naming what happens in the clip.",
  "tags": ["5-15 searchable keywords: colors, clothing, objects, vehicle types, activity verbs"],
  "activity_level": "none|low|moderate|high",
  "entities": [{"type": "person|vehicle|animal|object", "attributes": {"clothing_color": "...", "carrying": "...", "behavior": "...", "vehicle_type": "...", "vehicle_color": "...", "plate": "..."}}]
}

Guidance:
- description: prefer "a person in a red jacket with a backpack walks from left to right" over "a person is present".
- tags: one word or short phrase each. Include colors, clothing, carried objects, vehicle types, activity verbs. Example: ["person", "red jacket", "backpack", "walking", "left-to-right"].
- activity_level: none=empty scene or just lighting change; low=one stationary subject or passing vehicle; moderate=active movement or multiple subjects; high=rapid movement, many subjects, or unusual behavior.
- If the scene is genuinely empty, return description="Empty scene, no activity.", tags=["empty"], activity_level="none", entities=[]."""

SYSTEM_PROMPT = """You are a security operations AI analyst for the Ironsight surveillance platform.
Analyze this surveillance camera frame. You are given YOLO object detection results.

For every person in frame, look CAREFULLY at what they are holding or carrying in their hands or across their body. Identify specifically:
  - WEAPONS: rifle, shotgun, pistol, knife, bat, machete, club, pipe
  - TOOLS: crowbar, bolt cutter, hammer, ladder, drill, grinder
  - CONTAINERS: bag, backpack, duffel, box, crate, toolbox
  - OTHER: phone, radio, camera, clipboard
If you see a long, straight object held with two hands or slung across the body, consider RIFLE before camera or tool. A rifle is a CRITICAL threat.
If you are uncertain whether something is a weapon, flag it as a possible weapon rather than describing it as a camera or tool. Uncertainty about a weapon is itself a security concern.

Respond with ONLY a JSON object (no markdown, no explanation) with these exact fields:
{
  "threat_level": "critical|high|medium|low|none",
  "description": "1-2 sentence description. Name what the person is carrying explicitly.",
  "recommended_action": "specific action for the SOC operator to take",
  "false_positive_likelihood": 0.0 to 1.0,
  "objects": [{"type": "person|vehicle|animal|object", "attributes": {"badge_visible": true/false, "uniform": true/false, "carrying": "specific item name or null", "weapon_possible": true/false, "behavior": "description"}}]
}

Threat level guidance:
  - critical: weapon visible or strongly suspected, forced entry, active assault
  - high: unauthorized person after hours, carrying suspicious tools/containers, climbing fences
  - medium: person in restricted area, PPE violation, unusual behavior
  - low: routine activity, authorized-looking person
  - none: no person, vehicle-only, environmental only"""

# ── Model loading ──
model = None
processor = None
degraded_mode = False


def load_model():
    global model, processor, degraded_mode

    if not torch.cuda.is_available():
        logger.warning("No CUDA available. Running in DEGRADED mode (mock responses).")
        degraded_mode = True
        return

    # Log all available GPUs for clarity
    for i in range(torch.cuda.device_count()):
        name = torch.cuda.get_device_name(i)
        mem = torch.cuda.get_device_properties(i).total_memory / (1024**3)
        logger.info(f"GPU {i}: {name} ({mem:.1f}GB)")
    logger.info(f"Using device: {DEVICE}")

    # Check VRAM of the selected device
    device_idx = int(DEVICE.split(":")[1]) if DEVICE.startswith("cuda") else 0
    gpu_mem = torch.cuda.get_device_properties(device_idx).total_memory / (1024**3)
    if gpu_mem < MIN_VRAM_GB:
        logger.warning(
            f"GPU {device_idx} has {gpu_mem:.1f}GB VRAM — need {MIN_VRAM_GB}GB for {MODEL_NAME}. "
            "Running in DEGRADED mode (mock responses)."
        )
        degraded_mode = True
        return

    try:
        logger.info(f"Loading {MODEL_NAME} on {DEVICE}...")
        start = time.time()

        from transformers import Qwen2_5_VLForConditionalGeneration, AutoProcessor, BitsAndBytesConfig

        # int4 quantization via bitsandbytes. The 3B weights drop from ~7.2 GiB (fp16)
        # to ~2 GiB, leaving ~5 GiB free on the 3070 for vision-token activations and
        # KV cache — enough to run at 896px without the constant OOM loop we had at fp16.
        # nf4 + double-quant is the standard recipe for minimal accuracy loss.
        quant_config = BitsAndBytesConfig(
            load_in_4bit=True,
            bnb_4bit_quant_type="nf4",
            bnb_4bit_compute_dtype=torch.float16,
            bnb_4bit_use_double_quant=True,
        )

        model = Qwen2_5_VLForConditionalGeneration.from_pretrained(
            MODEL_NAME,
            quantization_config=quant_config,
            device_map={"": DEVICE},
        )
        processor = AutoProcessor.from_pretrained(MODEL_NAME)

        load_s = time.time() - start
        logger.info(f"Model loaded in {load_s:.1f}s")

        gpu_used = torch.cuda.memory_allocated(device_idx) / (1024**3)
        logger.info(f"GPU {device_idx} memory used: {gpu_used:.1f}GB")

    except Exception as e:
        logger.error(f"Failed to load model: {e}")
        logger.warning("Falling back to DEGRADED mode (mock responses)")
        degraded_mode = True


load_model()

app = FastAPI(title="Ironsight Qwen vLM Reasoning Service")


# pynvml is the canonical NVIDIA Management Library binding. Soft
# import so the service still boots if the wheel is unavailable; the
# GPU section of /health falls back to torch.cuda.mem_get_info in
# that case (memory only, no utilization or temperature).
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
            pass
    return out


@app.get("/health")
async def health():
    return {
        "status": "degraded" if degraded_mode else "ok",
        "model": MODEL_NAME,
        "device": DEVICE,
        "cuda_available": torch.cuda.is_available(),
        "gpu_name": torch.cuda.get_device_name(0) if torch.cuda.is_available() else None,
        "degraded": degraded_mode,
        **gpu_stats(),
    }


def mock_analysis(detections: list) -> dict:
    """Generate a plausible mock response when the model can't be loaded."""
    has_person = any(d.get("class") == "person" for d in detections)
    has_vehicle = any(d.get("class") in ("car", "truck", "bus", "vehicle") for d in detections)
    person_conf = max(
        (d.get("confidence", 0) for d in detections if d.get("class") == "person"),
        default=0,
    )

    if has_person and person_conf > 0.7:
        return {
            "threat_level": "high",
            "description": f"Person detected with {person_conf:.0%} confidence. Unable to perform detailed scene analysis (vLM unavailable).",
            "recommended_action": "Review camera feed manually. Verify if person is authorized.",
            "false_positive_likelihood": 0.3,
            "objects": [{"type": "person", "attributes": {"analysis": "unavailable — vLM in degraded mode"}}],
        }
    elif has_vehicle:
        return {
            "threat_level": "medium",
            "description": "Vehicle detected in camera view. Detailed analysis unavailable.",
            "recommended_action": "Monitor vehicle activity. Check if expected.",
            "false_positive_likelihood": 0.4,
            "objects": [{"type": "vehicle", "attributes": {"analysis": "unavailable — vLM in degraded mode"}}],
        }
    else:
        return {
            "threat_level": "low",
            "description": f"YOLO detected {len(detections)} object(s). Detailed analysis unavailable.",
            "recommended_action": "Review if warranted by camera analytics trigger.",
            "false_positive_likelihood": 0.5,
            "objects": [],
        }


def run_inference(pil_image: Image.Image, detections: list, site_context: str) -> dict:
    """Run actual Qwen inference."""
    from qwen_vl_utils import process_vision_info

    # Free any cached tensors from the previous inference before allocating new ones.
    # The RTX 3070 only has ~80 MiB free after model weights, so without this we OOM
    # on the second call onward.
    torch.cuda.empty_cache()

    # With nf4 int4 weights (~2 GiB), we have ~5 GiB headroom for activations, so
    # we can run at full 896px for clear weapon detail. Lower resolutions blurred
    # long objects enough to misread a rifle as a camera.
    MAX_EDGE = 896
    if max(pil_image.size) > MAX_EDGE:
        ratio = MAX_EDGE / max(pil_image.size)
        new_size = (int(pil_image.width * ratio), int(pil_image.height * ratio))
        pil_image = pil_image.resize(new_size, Image.LANCZOS)

    det_text = json.dumps(detections, indent=2) if detections else "No YOLO detections."
    user_prompt = f"YOLO detections:\n{det_text}"
    if site_context:
        user_prompt += f"\n\nSite context:\n{site_context}"
    user_prompt += "\n\nAnalyze this surveillance frame and respond with ONLY JSON."

    messages = [
        {"role": "system", "content": SYSTEM_PROMPT},
        {
            "role": "user",
            "content": [
                {"type": "image", "image": pil_image},
                {"type": "text", "text": user_prompt},
            ],
        },
    ]

    text = processor.apply_chat_template(messages, tokenize=False, add_generation_prompt=True)
    image_inputs, video_inputs = process_vision_info(messages)
    inputs = processor(
        text=[text],
        images=image_inputs,
        videos=video_inputs,
        padding=True,
        return_tensors="pt",
    ).to(model.device)

    try:
        with torch.inference_mode():
            output_ids = model.generate(**inputs, max_new_tokens=256, do_sample=False)

        # Trim input tokens from output
        generated_ids = output_ids[:, inputs.input_ids.shape[1]:]
        response_text = processor.batch_decode(generated_ids, skip_special_tokens=True)[0].strip()
    finally:
        # Always release the activation tensors so the next request has the full
        # ~80 MiB free budget again, even if generation raised.
        del inputs
        try:
            del output_ids, generated_ids
        except NameError:
            pass
        torch.cuda.empty_cache()

    # Parse JSON from response (handle markdown code blocks)
    json_match = re.search(r'\{[\s\S]*\}', response_text)
    if json_match:
        try:
            return json.loads(json_match.group())
        except json.JSONDecodeError:
            pass

    return {
        "threat_level": "medium",
        "description": response_text[:200],
        "recommended_action": "Review manually — AI response was not structured.",
        "false_positive_likelihood": 0.5,
        "objects": [],
    }


def run_video_inference(video_path: str, detections: list, site_context: str, mode: str = "assess") -> dict:
    """Run Qwen inference on a short video clip.

    Uses Qwen2.5-VL's native video support — the model internally samples frames
    across the clip, which lets it reason about motion (what the person is doing,
    how they're carrying something) rather than just a single frozen pose.

    mode='assess' (default): alarm/threat-focused system prompt
    mode='describe': neutral indexer prompt for background search indexing
    """
    from qwen_vl_utils import process_vision_info

    torch.cuda.empty_cache()

    system_prompt = DESCRIBE_PROMPT if mode == "describe" else SYSTEM_PROMPT

    if mode == "describe":
        user_prompt = (
            "Index this surveillance clip for later keyword search. Respond with ONLY JSON."
        )
        if detections:
            user_prompt += f"\n\nYOLO pre-detections: {json.dumps(detections)}"
    else:
        det_text = json.dumps(detections, indent=2) if detections else "No YOLO detections."
        user_prompt = f"YOLO detections:\n{det_text}"
        if site_context:
            user_prompt += f"\n\nSite context:\n{site_context}"
        user_prompt += (
            "\n\nThis is a short surveillance clip around the moment an alarm fired."
            " Describe what the person(s) do over the span of the clip — how they"
            " approach, what they carry, how they hold it. Respond with ONLY JSON."
        )

    # Qwen2.5-VL expects a local file URL for video input. max_pixels caps the
    # per-frame vision-token budget; nframes tells the sampler how many frames
    # to pull across the clip. 16 @ 640 leaves room on an 8 GB card with int4.
    messages = [
        {"role": "system", "content": system_prompt},
        {
            "role": "user",
            "content": [
                {
                    "type": "video",
                    # Pass the raw path (no file:// prefix). Decord on Windows
                    # tries to open the literal URI string; torchvision accepts
                    # both — raw path works for both backends.
                    "video": video_path,
                    "max_pixels": 640 * 360,
                    "nframes": 16,
                },
                {"type": "text", "text": user_prompt},
            ],
        },
    ]

    text = processor.apply_chat_template(messages, tokenize=False, add_generation_prompt=True)
    image_inputs, video_inputs = process_vision_info(messages)
    inputs = processor(
        text=[text],
        images=image_inputs,
        videos=video_inputs,
        padding=True,
        return_tensors="pt",
    ).to(model.device)

    try:
        with torch.inference_mode():
            output_ids = model.generate(**inputs, max_new_tokens=256, do_sample=False)
        generated_ids = output_ids[:, inputs.input_ids.shape[1]:]
        response_text = processor.batch_decode(generated_ids, skip_special_tokens=True)[0].strip()
    finally:
        del inputs
        try:
            del output_ids, generated_ids
        except NameError:
            pass
        torch.cuda.empty_cache()

    json_match = re.search(r'\{[\s\S]*\}', response_text)
    if json_match:
        try:
            return json.loads(json_match.group())
        except json.JSONDecodeError:
            pass

    return {
        "threat_level": "medium",
        "description": response_text[:200],
        "recommended_action": "Review manually — AI response was not structured.",
        "false_positive_likelihood": 0.5,
        "objects": [],
    }


@app.post("/analyze_video")
async def analyze_video(
    video: UploadFile = File(...),
    detections: str = Form(default="[]"),
    site_context: str = Form(default=""),
    mode: str = Form(default="assess"),
):
    """Analyze a short surveillance clip with Qwen vLM video input.

    mode='assess' (default) — threat/alarm analysis for the live pipeline.
    mode='describe' — neutral indexer output for the background search indexer.
    """
    start = time.time()

    if mode not in ("assess", "describe"):
        return JSONResponse(status_code=400, content={"error": f"invalid mode: {mode}"})

    video_bytes = await video.read()
    if len(video_bytes) < 1000:
        return JSONResponse(status_code=400, content={"error": "video too small"})

    try:
        det_list = json.loads(detections)
    except json.JSONDecodeError:
        det_list = []

    # Qwen's video sampler reads from a path, not bytes — stage to a temp file.
    tmp = tempfile.NamedTemporaryFile(prefix="qwen_clip_", suffix=".mp4", delete=False)
    try:
        tmp.write(video_bytes)
        tmp.close()

        if degraded_mode:
            result = mock_analysis(det_list)
            result["degraded"] = True
        else:
            try:
                result = run_video_inference(tmp.name, det_list, site_context, mode=mode)
                result["degraded"] = False
            except Exception as e:
                logger.error(f"Video inference error (mode={mode}): {e}")
                result = mock_analysis(det_list)
                result["degraded"] = True
                result["error"] = str(e)
    finally:
        try:
            os.unlink(tmp.name)
        except OSError:
            pass

    inference_ms = (time.time() - start) * 1000
    result["inference_ms"] = round(inference_ms, 1)
    result["model"] = MODEL_NAME
    result["mode"] = mode

    if mode == "describe":
        logger.info(
            f"Describe: activity={result.get('activity_level')} "
            f"tags={len(result.get('tags', []))} in {inference_ms:.0f}ms "
            f"{'(degraded)' if result.get('degraded') else ''}"
        )
    else:
        logger.info(
            f"Video analysis: threat={result.get('threat_level')} "
            f"fp={result.get('false_positive_likelihood', 0):.0%} "
            f"in {inference_ms:.0f}ms "
            f"{'(degraded)' if result.get('degraded') else ''}"
        )
    return result


@app.post("/analyze")
async def analyze(
    image: UploadFile = File(...),
    detections: str = Form(default="[]"),
    site_context: str = Form(default=""),
):
    """Analyze a surveillance frame with Qwen vLM."""
    start = time.time()

    # Parse image
    image_bytes = await image.read()
    try:
        pil_image = Image.open(io.BytesIO(image_bytes)).convert("RGB")
    except Exception as e:
        return JSONResponse(status_code=400, content={"error": f"Invalid image: {e}"})

    # Parse detections
    try:
        det_list = json.loads(detections)
    except json.JSONDecodeError:
        det_list = []

    # Run inference or mock
    if degraded_mode:
        result = mock_analysis(det_list)
        result["degraded"] = True
    else:
        try:
            result = run_inference(pil_image, det_list, site_context)
            result["degraded"] = False
        except Exception as e:
            logger.error(f"Inference error: {e}")
            result = mock_analysis(det_list)
            result["degraded"] = True
            result["error"] = str(e)

    inference_ms = (time.time() - start) * 1000
    result["inference_ms"] = round(inference_ms, 1)
    result["model"] = MODEL_NAME

    logger.info(
        f"Analysis: threat={result.get('threat_level')} "
        f"fp={result.get('false_positive_likelihood', 0):.0%} "
        f"in {inference_ms:.0f}ms "
        f"{'(degraded)' if result.get('degraded') else ''}"
    )

    return result
