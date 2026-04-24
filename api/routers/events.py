"""Security Events — Created from operator dispositions, visible on Customer Portal."""

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session
from database import get_db
from models.db import SecurityEvent
from models.schemas import SecurityEventCreate
from datetime import datetime
import time

router = APIRouter()

DISPOSITION_LABELS = {
    "false_positive_animal": "False Positive — Animal",
    "false_positive_weather": "False Positive — Weather",
    "false_positive_shadow": "False Positive — Shadow/Light",
    "false_positive_equipment": "False Positive — Equipment",
    "false_positive_other": "False Positive — Other",
    "verified_customer_notified": "Verified — Customer Notified",
    "verified_police_dispatched": "Verified — Police Dispatched",
    "verified_guard_responded": "Verified — Guard Responded",
    "verified_no_threat": "Verified — No Active Threat",
    "verified_other": "Verified — Other",
}


@router.post("/events")
async def create_security_event(body: SecurityEventCreate, db: Session = Depends(get_db)):
    event_id = f"EVT-{datetime.now().year}-{str(int(time.time()))[-4:]}"
    event = SecurityEvent(
        id=event_id,
        alarm_id=body.alarm_id,
        site_id=body.site_id,
        camera_id=body.camera_id,
        disposition_code=body.disposition_code,
        disposition_label=DISPOSITION_LABELS.get(body.disposition_code, body.disposition_code),
        operator_notes=body.operator_notes,
        action_log=[e.model_dump() for e in body.action_log],
        escalation_depth=body.escalation_depth,
        ts=int(time.time() * 1000),
        resolved_at=int(time.time() * 1000),
    )
    db.add(event)
    db.commit()
    return {"event_id": event_id}


@router.get("/events")
async def list_events(site_id: str = None, viewed: bool = None, db: Session = Depends(get_db)):
    q = db.query(SecurityEvent)
    if site_id:
        q = q.filter(SecurityEvent.site_id == site_id)
    if viewed is not None:
        q = q.filter(SecurityEvent.viewed_by_customer == viewed)
    events = q.order_by(SecurityEvent.resolved_at.desc()).all()
    return [
        {
            "event_id": e.id, "alarm_id": e.alarm_id, "site_id": e.site_id,
            "camera_id": e.camera_id, "severity": e.severity,
            "disposition_code": e.disposition_code, "disposition_label": e.disposition_label,
            "operator_notes": e.operator_notes, "action_log": e.action_log,
            "escalation_depth": e.escalation_depth,
            "ts": e.ts, "resolved_at": e.resolved_at,
            "viewed_by_customer": e.viewed_by_customer,
        }
        for e in events
    ]


@router.put("/events/{event_id}/viewed")
async def mark_viewed(event_id: str, db: Session = Depends(get_db)):
    event = db.query(SecurityEvent).filter(SecurityEvent.id == event_id).first()
    if event:
        event.viewed_by_customer = True
        db.commit()
        return {"ok": True}
    return {"error": "not found"}
