"""
Pydantic models for all Ironsight API request/response schemas.
"""

from pydantic import BaseModel
from typing import Optional, Literal
from datetime import datetime


# ── Operator Presence ──

class OperatorPresenceUpdate(BaseModel):
    status: Literal["available", "engaged", "wrap_up", "away"]


class OperatorPresenceResponse(BaseModel):
    operator_id: str
    callsign: str
    name: str
    status: Literal["available", "engaged", "wrap_up", "away"]
    active_alarm_id: Optional[str] = None
    last_seen: datetime


# ── Dispatch Queue ──

class QueueStatus(BaseModel):
    depth: int
    oldest_ts: Optional[int] = None  # Unix ms


class AlarmDispatch(BaseModel):
    alarm_id: str
    site_id: str
    camera_id: str
    camera_name: str
    site_name: str
    severity: Literal["critical", "high", "medium", "low"]
    type: str  # "person_detected", "vehicle_detected", "motion_detected"
    description: str
    clip_url: str
    snapshot_url: str
    ts: int  # Unix ms


# ── Security Events ──

class ActionLogEntry(BaseModel):
    ts: int
    text: str
    auto: bool = False


class SecurityEventCreate(BaseModel):
    alarm_id: str
    site_id: str
    camera_id: str
    disposition_code: str
    operator_notes: str
    action_log: list[ActionLogEntry]
    escalation_depth: int = 0
    clip_bookmark_id: Optional[str] = None


class SecurityEventResponse(BaseModel):
    event_id: str
    alarm_id: str
    site_id: str
    site_name: str
    camera_id: str
    camera_name: str
    disposition_code: str
    disposition_label: str
    operator_id: str
    operator_callsign: str
    operator_notes: str
    action_log: list[ActionLogEntry]
    escalation_depth: int
    severity: str
    ts: int  # alarm timestamp
    resolved_at: int  # resolution timestamp
    clip_url: str
    viewed_by_customer: bool = False


# ── Evidence Sharing ──

class EvidenceShareCreate(BaseModel):
    incident_id: str
    expires_in: Literal["1h", "1d", "1w", "1m", "never"]


class EvidenceShareResponse(BaseModel):
    token: str
    url: str
    expires_at: Optional[datetime] = None
    cloud_sync_status: Literal["syncing", "complete", "failed"]


# ── Safety / vLM ──

class SafetyFindingValidation(BaseModel):
    valid: bool
    correction: Optional[str] = None


class AICorrection(BaseModel):
    finding_id: str
    original_caption: str
    correction_type: str
