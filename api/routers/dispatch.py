"""Dispatch — FIFO alarm queue and operator routing."""

from fastapi import APIRouter
from collections import deque
from models.schemas import QueueStatus, AlarmDispatch

router = APIRouter()

# ── FIFO alarm queue (replace with Redis sorted set in production) ──
_alarm_queue: deque[dict] = deque()


@router.get("/dispatch/queue")
async def get_queue_status() -> QueueStatus:
    """Return current queue depth and oldest alarm timestamp."""
    if len(_alarm_queue) == 0:
        return QueueStatus(depth=0, oldest_ts=None)
    return QueueStatus(
        depth=len(_alarm_queue),
        oldest_ts=_alarm_queue[0].get("ts"),
    )


@router.post("/dispatch/enqueue")
async def enqueue_alarm(alarm: AlarmDispatch):
    """
    Called by the AI detection pipeline when a verified alarm is ready for dispatch.

    Flow:
    1. Check for an available operator (longest idle first)
    2. If found → push directly to their WebSocket via /ws/alerts/{operator_id}
    3. If none available → add to FIFO queue
    """
    # TODO: Check operator presence table for 'available' operators
    # TODO: If available, dispatch via WebSocket
    # TODO: If none, enqueue

    _alarm_queue.append(alarm.model_dump())
    return {"queued": True, "position": len(_alarm_queue)}


@router.post("/dispatch/dequeue")
async def dequeue_alarm():
    """
    Called when an operator becomes available (finishes a disposition).

    Returns the oldest queued alarm, or null if queue is empty.
    """
    if len(_alarm_queue) == 0:
        return {"alarm": None}
    alarm = _alarm_queue.popleft()
    return {"alarm": alarm}
