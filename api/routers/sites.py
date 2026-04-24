"""Sites — CRUD, camera assignments, user assignments, SOPs."""

from fastapi import APIRouter, Depends
from pydantic import BaseModel
from sqlalchemy.orm import Session
from database import get_db
from models.db import Site, Camera, SiteSOP, User, gen_id
from datetime import datetime

router = APIRouter()


# ── Request schemas ──

class SiteCreate(BaseModel):
    name: str
    address: str = ""
    company_id: str
    latitude: float | None = None
    longitude: float | None = None


class CameraCreate(BaseModel):
    name: str
    onvif_address: str = ""
    manufacturer: str = ""
    model: str = ""

class CameraAssign(BaseModel):
    camera_id: str
    location_label: str = ""


class UserAssign(BaseModel):
    user_id: str


class SOPCreate(BaseModel):
    site_id: str
    title: str
    category: str = "access"
    priority: str = "normal"
    steps: list[str] = []
    contacts: list[dict] = []
    updated_by: str = ""


class SOPUpdate(BaseModel):
    title: str | None = None
    category: str | None = None
    priority: str | None = None
    steps: list[str] | None = None
    contacts: list[dict] | None = None
    updated_by: str | None = None


# ── Sites ──

@router.get("/sites")
async def list_sites(db: Session = Depends(get_db)):
    sites = db.query(Site).all()
    return [
        {
            "id": s.id, "name": s.name, "status": s.status,
            "address": s.address, "company_id": s.organization_id,
            "cameras_online": sum(1 for c in s.cameras if c.status == "online"),
            "cameras_total": len(s.cameras),
            "compliance_score": 85,
            "open_incidents": 0,
            "workers_on_site": 0,
            "last_activity": datetime.utcnow().isoformat(),
            "trend": "flat",
        }
        for s in sites
    ]


@router.get("/sites/{site_id}")
async def get_site(site_id: str, db: Session = Depends(get_db)):
    site = db.query(Site).filter(Site.id == site_id).first()
    if not site:
        return {"error": "not found"}
    return {
        "id": site.id, "name": site.name, "status": site.status,
        "address": site.address,
        "latitude": site.latitude, "longitude": site.longitude,
        "company_id": site.organization_id,
        "cameras": [
            {"id": c.id, "name": c.name, "location": c.location, "status": c.status,
             "has_alert": False, "stream_url": c.stream_url}
            for c in site.cameras
        ],
        "cameras_online": sum(1 for c in site.cameras if c.status == "online"),
        "cameras_total": len(site.cameras),
        "compliance_score": 85,
        "open_incidents": 0,
        "workers_on_site": 0,
        "last_activity": datetime.utcnow().isoformat(),
        "trend": "flat",
        "risk_notes": site.site_notes or [],
        "ppe_breakdown": {"hard_hat": 89, "harness": 76, "hi_vis": 94, "boots": 97, "gloves": 82},
        "compliance_history": [],
    }


@router.post("/sites")
async def create_site(body: SiteCreate, db: Session = Depends(get_db)):
    # Generate site ID from name initials + random number
    initials = "".join(w[0] for w in body.name.split() if w).upper()
    import random
    site_id = f"{initials}-{random.randint(100, 999)}"

    site = Site(
        id=site_id, name=body.name, address=body.address,
        organization_id=body.company_id,
        latitude=body.latitude, longitude=body.longitude,
        status="active", site_notes=[],
    )
    db.add(site)
    db.commit()
    db.refresh(site)
    return {
        "id": site.id, "name": site.name, "status": "active",
        "address": site.address, "company_id": body.company_id,
        "cameras_online": 0, "cameras_total": 0,
        "compliance_score": 0, "open_incidents": 0,
        "workers_on_site": 0,
        "last_activity": datetime.utcnow().isoformat(),
        "trend": "flat",
    }


@router.delete("/sites/{site_id}")
async def delete_site(site_id: str, db: Session = Depends(get_db)):
    site = db.query(Site).filter(Site.id == site_id).first()
    if site:
        # Delete cameras and SOPs first
        db.query(Camera).filter(Camera.site_id == site_id).delete()
        db.query(SiteSOP).filter(SiteSOP.site_id == site_id).delete()
        db.delete(site)
        db.commit()
    return {"ok": True}


# ── Cameras ──

# ════════════════════════════════════════
# Master Camera Registry
# ════════════════════════════════════════

@router.get("/cameras")
async def list_all_cameras(db: Session = Depends(get_db)):
    """Master list of all cameras in the system — assigned and unassigned."""
    cameras = db.query(Camera).all()
    return [
        {
            "id": c.id, "name": c.name, "site_id": c.site_id,
            "location": c.location, "status": c.status,
            "onvif_address": c.onvif_address or "",
            "manufacturer": c.manufacturer or "",
            "model": c.model or "",
            "stream_url": c.stream_url,
            "recording": False,
        }
        for c in cameras
    ]


@router.post("/cameras")
async def create_camera(body: CameraCreate, db: Session = Depends(get_db)):
    """Add a new camera to the master registry (unassigned)."""
    cam = Camera(
        id=f"cam-{gen_id()}",
        name=body.name,
        site_id=None,
        onvif_address=body.onvif_address,
        manufacturer=body.manufacturer,
        model=body.model,
        status="online",
    )
    db.add(cam)
    db.commit()
    return {
        "id": cam.id, "name": cam.name, "site_id": None,
        "location": "", "status": "online",
        "onvif_address": cam.onvif_address,
        "manufacturer": cam.manufacturer, "model": cam.model,
    }


@router.delete("/cameras/{camera_id}")
async def delete_camera(camera_id: str, db: Session = Depends(get_db)):
    """Remove a camera from the system entirely."""
    cam = db.query(Camera).filter(Camera.id == camera_id).first()
    if cam:
        db.delete(cam)
        db.commit()
    return {"ok": True}


# ════════════════════════════════════════
# Site Camera Assignments
# ════════════════════════════════════════

@router.get("/sites/{site_id}/cameras")
async def get_site_cameras(site_id: str, db: Session = Depends(get_db)):
    cameras = db.query(Camera).filter(Camera.site_id == site_id).all()
    return [
        {"id": c.id, "name": c.name, "location": c.location, "status": c.status,
         "has_alert": False, "stream_url": c.stream_url}
        for c in cameras
    ]


@router.get("/sites/{site_id}/camera-assignments")
async def get_camera_assignments(site_id: str, db: Session = Depends(get_db)):
    cameras = db.query(Camera).filter(Camera.site_id == site_id).all()
    return [
        {
            "site_id": site_id, "camera_id": c.id,
            "camera_name": c.name, "location_label": c.location,
            "assigned_at": datetime.utcnow().isoformat(),
        }
        for c in cameras
    ]


@router.post("/sites/{site_id}/camera-assignments")
async def assign_camera(site_id: str, body: CameraAssign, db: Session = Depends(get_db)):
    """Assign an existing camera from the master registry to a site."""
    cam = db.query(Camera).filter(Camera.id == body.camera_id).first()
    if not cam:
        return {"error": "Camera not found in master registry"}
    cam.site_id = site_id
    cam.location = body.location_label
    db.commit()
    return {
        "site_id": site_id, "camera_id": cam.id,
        "camera_name": cam.name, "location_label": cam.location,
        "assigned_at": datetime.utcnow().isoformat(),
    }


@router.delete("/sites/{site_id}/camera-assignments/{camera_id}")
async def unassign_camera(site_id: str, camera_id: str, db: Session = Depends(get_db)):
    """Unassign a camera from a site — camera stays in master registry."""
    cam = db.query(Camera).filter(Camera.id == camera_id, Camera.site_id == site_id).first()
    if cam:
        cam.site_id = None
        cam.location = ""
        db.commit()
    return {"ok": True}


# ── Site Users ──

@router.get("/sites/{site_id}/users")
async def get_site_users(site_id: str, db: Session = Depends(get_db)):
    # Find users whose assigned_site_ids contains this site_id
    # SQLite JSON support is limited, so we filter in Python
    all_users = db.query(User).all()
    assigned = [u for u in all_users if site_id in (u.assigned_site_ids or [])]
    return [
        {
            "site_id": site_id, "user_id": u.id,
            "user_name": u.name, "user_email": u.email,
            "role": u.role, "assigned_at": "2026-01-01T00:00:00Z",
        }
        for u in assigned
    ]


@router.post("/sites/{site_id}/users")
async def assign_user_to_site(site_id: str, body: UserAssign, db: Session = Depends(get_db)):
    user = db.query(User).filter(User.id == body.user_id).first()
    if user:
        sites = user.assigned_site_ids or []
        if site_id not in sites:
            user.assigned_site_ids = sites + [site_id]
            db.commit()
    return {
        "site_id": site_id, "user_id": body.user_id,
        "user_name": user.name if user else "", "user_email": user.email if user else "",
        "role": user.role if user else "viewer",
        "assigned_at": datetime.utcnow().isoformat(),
    }


@router.delete("/sites/{site_id}/users/{user_id}")
async def unassign_user(site_id: str, user_id: str, db: Session = Depends(get_db)):
    user = db.query(User).filter(User.id == user_id).first()
    if user and user.assigned_site_ids:
        user.assigned_site_ids = [s for s in user.assigned_site_ids if s != site_id]
        db.commit()
    return {"ok": True}


# ── SOPs ──

@router.get("/sites/{site_id}/sops")
async def get_site_sops(site_id: str, db: Session = Depends(get_db)):
    sops = db.query(SiteSOP).filter(SiteSOP.site_id == site_id).all()
    return [
        {
            "id": s.id, "site_id": s.site_id, "title": s.title,
            "category": s.category, "priority": s.priority,
            "steps": s.steps or [], "contacts": s.contacts or [],
            "updated_at": s.updated_at, "updated_by": s.updated_by,
        }
        for s in sops
    ]


@router.post("/sites/{site_id}/sops")
async def create_sop(site_id: str, body: SOPCreate, db: Session = Depends(get_db)):
    sop = SiteSOP(
        id=f"sop-{gen_id()}", site_id=site_id,
        title=body.title, category=body.category, priority=body.priority,
        steps=body.steps, contacts=body.contacts,
        updated_at=datetime.utcnow().isoformat(), updated_by=body.updated_by,
    )
    db.add(sop)
    db.commit()
    return {
        "id": sop.id, "site_id": site_id, "title": sop.title,
        "category": sop.category, "priority": sop.priority,
        "steps": sop.steps, "contacts": sop.contacts,
        "updated_at": sop.updated_at, "updated_by": sop.updated_by,
    }


@router.put("/sops/{sop_id}")
async def update_sop(sop_id: str, body: SOPUpdate, db: Session = Depends(get_db)):
    sop = db.query(SiteSOP).filter(SiteSOP.id == sop_id).first()
    if not sop:
        return {"error": "not found"}
    if body.title is not None: sop.title = body.title
    if body.category is not None: sop.category = body.category
    if body.priority is not None: sop.priority = body.priority
    if body.steps is not None: sop.steps = body.steps
    if body.contacts is not None: sop.contacts = body.contacts
    if body.updated_by is not None: sop.updated_by = body.updated_by
    sop.updated_at = datetime.utcnow().isoformat()
    db.commit()
    return {
        "id": sop.id, "site_id": sop.site_id, "title": sop.title,
        "category": sop.category, "priority": sop.priority,
        "steps": sop.steps, "contacts": sop.contacts,
        "updated_at": sop.updated_at, "updated_by": sop.updated_by,
    }


@router.delete("/sops/{sop_id}")
async def delete_sop(sop_id: str, db: Session = Depends(get_db)):
    sop = db.query(SiteSOP).filter(SiteSOP.id == sop_id).first()
    if sop:
        db.delete(sop)
        db.commit()
    return {"ok": True}


# ── Site Locks (in-memory for now) ──

@router.get("/sites/locks")
async def get_site_locks():
    return []


@router.post("/sites/{site_id}/lock")
async def lock_site(site_id: str):
    return {"ok": True}


@router.post("/sites/{site_id}/unlock")
async def unlock_site(site_id: str):
    return {"ok": True}
