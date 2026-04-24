"""
Ironsight API — FastAPI Backend
Security monitoring, operator dispatch, and customer portal services.
"""

from fastapi import FastAPI, Depends
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel
from sqlalchemy.orm import Session
import hashlib

from database import get_db, create_tables
from models.db import User, Operator
from auth import create_token
from routers import sites, alerts, events, operators, dispatch, evidence, safety, companies

app = FastAPI(
    title="Ironsight API",
    description="Construction site security & safety intelligence platform",
    version="1.0.0",
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["http://localhost:3000"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)


# ── Create tables on startup ──
@app.on_event("startup")
def startup():
    create_tables()


# ── Auth ──
class LoginRequest(BaseModel):
    username: str  # email
    password: str


@app.post("/auth/login")
async def login(body: LoginRequest, db: Session = Depends(get_db)):
    # Check users table
    user = db.query(User).filter(User.email == body.username).first()
    if user and hashlib.sha256(body.password.encode()).hexdigest() == user.password_hash:
        token = create_token({"sub": user.id, "role": user.role, "name": user.name})
        return {
            "token": token,
            "user": {
                "id": user.id, "username": user.email, "name": user.name,
                "role": user.role, "email": user.email,
                "company": user.organization_id,
            },
        }

    # Check operators table
    op = db.query(Operator).filter(Operator.email == body.username).first()
    if op and hashlib.sha256(body.password.encode()).hexdigest() == op.password_hash:
        token = create_token({"sub": op.id, "role": "soc_operator", "name": op.name})
        return {
            "token": token,
            "user": {
                "id": op.id, "username": op.email, "name": op.name,
                "role": "soc_operator", "email": op.email,
            },
        }

    return {"error": "Invalid credentials"}, 401


# ── Routes ──
app.include_router(companies.router, prefix="/api/v1", tags=["Companies"])
app.include_router(sites.router, prefix="/api/v1", tags=["Sites"])
app.include_router(alerts.router, prefix="/api/v1", tags=["Alerts"])
app.include_router(events.router, prefix="/api/v1", tags=["Security Events"])
app.include_router(operators.router, prefix="/api/v1", tags=["Operators"])
app.include_router(dispatch.router, prefix="/api/v1", tags=["Dispatch"])
app.include_router(evidence.router, prefix="/api/v1", tags=["Evidence"])
app.include_router(safety.router, prefix="/api/v1", tags=["Safety / vLM"])


@app.get("/health")
async def health():
    return {"status": "ok", "service": "ironsight-api"}
