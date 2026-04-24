@echo off
set QWEN_MODEL=Qwen/Qwen2.5-VL-3B-Instruct
set QWEN_DEVICE=cuda:0
set QWEN_MIN_VRAM_GB=6
set PYTORCH_CUDA_ALLOC_CONF=expandable_segments:True
python -m uvicorn server:app --host 0.0.0.0 --port 8502
