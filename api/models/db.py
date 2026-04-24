"""SQLAlchemy ORM models for all Ironsight entities."""

from sqlalchemy import Column, String, Integer, Float, Boolean, Text, ForeignKey, JSON
from sqlalchemy.orm import relationship
from database import Base
import time
import uuid


def gen_id():
    return str(uuid.uuid4())[:8]


# ── Organizations (Customers) ──

class Organization(Base):
    __tablename__ = "organizations"

    id = Column(String, primary_key=True, default=gen_id)
    name = Column(String, nullable=False)
    plan = Column(String, default="professional")  # starter, professional, enterprise
    contact_name = Column(String, default="")
    contact_email = Column(String, default="")
    logo_url = Column(String, nullable=True)
    features = Column(JSON, default=lambda: {
        "vlm_safety": True,
        "semantic_search": True,
        "evidence_sharing": True,
        "global_ai_training": True,
    })
    created_at = Column(String, default=lambda: __import__("datetime").datetime.utcnow().isoformat())

    sites = relationship("Site", back_populates="organization")
    users = relationship("User", back_populates="organization")


# ── Sites ──

class Site(Base):
    __tablename__ = "sites"

    id = Column(String, primary_key=True)  # e.g. "TX-203"
    name = Column(String, nullable=False)
    address = Column(String, default="")
    organization_id = Column(String, ForeignKey("organizations.id"), nullable=False)
    latitude = Column(Float, nullable=True)
    longitude = Column(Float, nullable=True)
    status = Column(String, default="active")  # active, idle, critical
    monitoring_start = Column(String, default="18:00")  # HH:MM — monitoring hours start
    monitoring_end = Column(String, default="06:00")     # HH:MM — monitoring hours end

    organization = relationship("Organization", back_populates="sites")
    cameras = relationship("Camera", back_populates="site")
    sops = relationship("SiteSOP", back_populates="site")
    site_notes = Column(JSON, default=list)  # ["Gate 3 broken", "Guard dog after 10PM"]


# ── Cameras ──

class Camera(Base):
    __tablename__ = "cameras"

    id = Column(String, primary_key=True, default=gen_id)
    name = Column(String, nullable=False)
    site_id = Column(String, ForeignKey("sites.id"), nullable=True)  # null = unassigned
    location = Column(String, default="")  # "North Perimeter", "Gate A"
    status = Column(String, default="online")  # online, offline, degraded
    stream_url = Column(String, nullable=True)  # HLS URL from NVR proxy
    onvif_address = Column(String, nullable=True)
    manufacturer = Column(String, default="")
    model = Column(String, default="")

    site = relationship("Site", back_populates="cameras")


# ── Site SOPs (Call Trees + Response Procedures) ──

class SiteSOP(Base):
    __tablename__ = "site_sops"

    id = Column(String, primary_key=True, default=gen_id)
    site_id = Column(String, ForeignKey("sites.id"), nullable=False)
    title = Column(String, nullable=False)
    category = Column(String, default="access")  # access, emergency, safety, equipment, general
    priority = Column(String, default="normal")   # critical, high, normal
    steps = Column(JSON, default=list)             # ordered procedure steps
    contacts = Column(JSON, default=list)          # [{name, role, phone, email}]
    updated_at = Column(String, default=lambda: __import__("datetime").datetime.utcnow().isoformat())
    updated_by = Column(String, default="")

    site = relationship("Site", back_populates="sops")


# ── Users (Site Managers, Executives, Admins) ──

class User(Base):
    __tablename__ = "users"

    id = Column(String, primary_key=True, default=gen_id)
    name = Column(String, nullable=False)
    email = Column(String, unique=True, nullable=False)
    phone = Column(String, nullable=True)
    password_hash = Column(String, nullable=False)
    role = Column(String, default="site_manager")  # admin, soc_operator, soc_supervisor, site_manager, customer
    organization_id = Column(String, ForeignKey("organizations.id"), nullable=True)
    assigned_site_ids = Column(JSON, default=list)  # ACL: which sites this user can see

    organization = relationship("Organization", back_populates="users")


# ── SOC Operators ──

class Operator(Base):
    __tablename__ = "operators"

    id = Column(String, primary_key=True, default=gen_id)
    name = Column(String, nullable=False)
    callsign = Column(String, nullable=False)  # "OP-1", "OP-2"
    email = Column(String, nullable=True)
    password_hash = Column(String, default="")
    status = Column(String, default="available")  # available, engaged, wrap_up, away
    active_alarm_id = Column(String, nullable=True)
    last_active = Column(Integer, default=lambda: int(time.time() * 1000))


# ── Security Events (SOC → Customer Portal bridge) ──

class SecurityEvent(Base):
    __tablename__ = "security_events"

    id = Column(String, primary_key=True)  # EVT-2026-XXXX
    alarm_id = Column(String, nullable=False)
    site_id = Column(String, ForeignKey("sites.id"), nullable=False)
    camera_id = Column(String, nullable=False)
    severity = Column(String, default="high")
    type = Column(String, default="person_detected")
    description = Column(Text, default="")
    disposition_code = Column(String, nullable=False)
    disposition_label = Column(String, default="")
    operator_id = Column(String, nullable=True)
    operator_callsign = Column(String, default="")
    operator_notes = Column(Text, default="")
    action_log = Column(JSON, default=list)
    escalation_depth = Column(Integer, default=0)
    clip_url = Column(String, default="")
    clip_bookmark_id = Column(String, nullable=True)
    ts = Column(Integer, nullable=False)           # alarm timestamp (Unix ms)
    resolved_at = Column(Integer, nullable=False)   # resolution timestamp (Unix ms)
    viewed_by_customer = Column(Boolean, default=False)
