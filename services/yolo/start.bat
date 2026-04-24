@echo off
set YOLO_MODEL=yolo11m.pt
set PPE_MODEL=models/ppe.pt
set YOLO_DEVICE=cuda:1
python -m uvicorn server:app --host 0.0.0.0 --port 8501
