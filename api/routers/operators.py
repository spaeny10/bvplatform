"""Operators — Presence tracking, roster, and auth."""

from fastapi import APIRouter, Depends
from sqlalchemy.orm import Session
from database import get_db
from models.db import Operator
import time

router = APIRouter()


@router.get("/operators")
async def list_operators(db: Session = Depends(get_db)):
    ops = db.query(Operator).all()
    return [
        {"id": o.id, "name": o.name, "callsign": o.callsign,
         "status": o.status, "active_alarm_id": o.active_alarm_id}
        for o in ops
    ]


@router.get("/operators/current")
async def get_current_operator(db: Session = Depends(get_db)):
    # TODO: extract from JWT — for now return first available
    op = db.query(Operator).filter(Operator.status != "away").first()
    if not op:
        op = db.query(Operator).first()
    return {
        "id": op.id, "name": op.name, "callsign": op.callsign,
        "status": op.status,
    }


@router.put("/operators/{operator_id}/presence")
async def update_presence(operator_id: str, db: Session = Depends(get_db)):
    op = db.query(Operator).filter(Operator.id == operator_id).first()
    if op:
        op.last_active = int(time.time() * 1000)
        db.commit()
    return {"ok": True}


@router.get("/operators/{operator_id}/handoffs")
async def get_pending_handoffs(operator_id: str):
    return []
