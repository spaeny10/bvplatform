"""Alerts — Security alarm feed and WebSocket dispatch."""

from fastapi import APIRouter, WebSocket, WebSocketDisconnect
import json
import asyncio

router = APIRouter()

# ── In-memory alarm state (replace with Redis/DB in production) ──
_connected_operators: dict[str, WebSocket] = {}


@router.get("/alerts")
async def list_alerts(site_id: str = None, severity: str = None, limit: int = 50):
    """Return recent alerts (REST fallback for initial page load)."""
    # TODO: Query from database
    return []


@router.websocket("/ws/alerts/{operator_id}")
async def alert_websocket(websocket: WebSocket, operator_id: str):
    """
    Per-operator WebSocket for directed alarm dispatch.

    The dispatch service pushes alarms to the longest-idle available operator.
    This endpoint is the delivery channel for that specific operator.
    """
    await websocket.accept()
    _connected_operators[operator_id] = websocket

    try:
        while True:
            # Keep-alive: listen for client messages (presence pings, acks)
            data = await websocket.receive_text()
            msg = json.loads(data)

            if msg.get("type") == "presence":
                # Operator status update (available/engaged/away)
                pass  # TODO: Update operator presence in dispatch service

            elif msg.get("type") == "ack":
                # Operator acknowledged/claimed an alarm
                pass  # TODO: Remove from queue, update alarm state

    except WebSocketDisconnect:
        _connected_operators.pop(operator_id, None)


async def dispatch_alarm_to_operator(operator_id: str, alarm: dict):
    """Push an alarm to a specific operator's WebSocket."""
    ws = _connected_operators.get(operator_id)
    if ws:
        await ws.send_json({"type": "alarm", "data": alarm})
