"""Companies (Organizations) and their users."""

from fastapi import APIRouter, Depends
from pydantic import BaseModel
from sqlalchemy.orm import Session
from database import get_db
from models.db import Organization, User, gen_id
from datetime import datetime
import hashlib

router = APIRouter()


# ── Request schemas ──

class CompanyCreate(BaseModel):
    name: str
    plan: str = "professional"
    contact_name: str = ""
    contact_email: str = ""
    logo_url: str | None = None


class UserCreate(BaseModel):
    name: str
    email: str
    phone: str = ""
    role: str = "site_manager"
    company_id: str
    assigned_site_ids: list[str] = []
    password: str = "demo123"


# ── Companies ──

@router.get("/companies")
async def list_companies(db: Session = Depends(get_db)):
    orgs = db.query(Organization).all()
    return [
        {
            "id": o.id, "name": o.name, "plan": o.plan,
            "contact_name": o.contact_name, "contact_email": o.contact_email,
            "logo_url": o.logo_url,
            "created_at": o.created_at,
        }
        for o in orgs
    ]


@router.get("/companies/{company_id}")
async def get_company(company_id: str, db: Session = Depends(get_db)):
    org = db.query(Organization).filter(Organization.id == company_id).first()
    if not org:
        return {"error": "not found"}
    return {
        "id": org.id, "name": org.name, "plan": org.plan,
        "contact_name": org.contact_name, "contact_email": org.contact_email,
        "logo_url": org.logo_url, "created_at": org.created_at,
    }


@router.post("/companies")
async def create_company(body: CompanyCreate, db: Session = Depends(get_db)):
    org = Organization(
        id=f"co-{gen_id()}",
        name=body.name,
        plan=body.plan,
        contact_name=body.contact_name,
        contact_email=body.contact_email,
        logo_url=body.logo_url,
        created_at=datetime.utcnow().isoformat(),
    )
    db.add(org)
    db.commit()
    db.refresh(org)
    return {
        "id": org.id, "name": org.name, "plan": org.plan,
        "contact_name": org.contact_name, "contact_email": org.contact_email,
        "logo_url": org.logo_url, "created_at": org.created_at,
    }


@router.put("/companies/{company_id}")
async def update_company(company_id: str, body: CompanyCreate, db: Session = Depends(get_db)):
    org = db.query(Organization).filter(Organization.id == company_id).first()
    if not org:
        return {"error": "not found"}
    org.name = body.name
    org.plan = body.plan
    org.contact_name = body.contact_name
    org.contact_email = body.contact_email
    if body.logo_url is not None:
        org.logo_url = body.logo_url
    db.commit()
    return {"id": org.id, "name": org.name, "plan": org.plan,
            "contact_name": org.contact_name, "contact_email": org.contact_email}


@router.delete("/companies/{company_id}")
async def delete_company(company_id: str, db: Session = Depends(get_db)):
    org = db.query(Organization).filter(Organization.id == company_id).first()
    if org:
        db.query(User).filter(User.organization_id == company_id).delete()
        db.delete(org)
        db.commit()
    return {"ok": True}


# ── Company Users (update/delete) ──

@router.put("/companies/{company_id}/users/{user_id}")
async def update_user(company_id: str, user_id: str, body: UserCreate, db: Session = Depends(get_db)):
    user = db.query(User).filter(User.id == user_id).first()
    if not user:
        return {"error": "not found"}
    user.name = body.name
    user.email = body.email
    user.phone = body.phone
    user.role = body.role
    db.commit()
    return {"id": user.id, "name": user.name, "email": user.email,
            "phone": user.phone or "", "role": user.role}


@router.delete("/companies/{company_id}/users/{user_id}")
async def delete_user(company_id: str, user_id: str, db: Session = Depends(get_db)):
    user = db.query(User).filter(User.id == user_id).first()
    if user:
        db.delete(user)
        db.commit()
    return {"ok": True}


# ── Company Users ──

@router.get("/companies/{company_id}/users")
async def list_company_users(company_id: str, db: Session = Depends(get_db)):
    users = db.query(User).filter(User.organization_id == company_id).all()
    return [
        {
            "id": u.id, "company_id": u.organization_id, "name": u.name,
            "email": u.email, "phone": u.phone or "", "role": u.role,
            "assigned_site_ids": u.assigned_site_ids or [],
            "created_at": "2026-01-01T00:00:00Z",
        }
        for u in users
    ]


@router.post("/companies/{company_id}/users")
async def create_company_user(company_id: str, body: UserCreate, db: Session = Depends(get_db)):
    user = User(
        id=gen_id(),
        name=body.name,
        email=body.email,
        phone=body.phone,
        password_hash=hashlib.sha256(body.password.encode()).hexdigest(),
        role=body.role,
        organization_id=company_id,
        assigned_site_ids=body.assigned_site_ids,
    )
    db.add(user)
    db.commit()
    db.refresh(user)
    return {
        "id": user.id, "company_id": company_id, "name": user.name,
        "email": user.email, "phone": user.phone or "",
        "role": user.role,
        "assigned_site_ids": user.assigned_site_ids or [],
        "created_at": datetime.utcnow().isoformat(),
    }
