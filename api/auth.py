"""JWT authentication for Ironsight API."""

from fastapi import Depends, HTTPException, status
from fastapi.security import HTTPBearer, HTTPAuthorizationCredentials
from jose import JWTError, jwt
import hashlib
from datetime import datetime, timedelta
from sqlalchemy.orm import Session
from database import get_db
from models.db import User, Operator

SECRET_KEY = "ironsight-dev-secret-change-in-production"
ALGORITHM = "HS256"
TOKEN_EXPIRE_HOURS = 8

security = HTTPBearer(auto_error=False)


def create_token(data: dict) -> str:
    payload = data.copy()
    payload["exp"] = datetime.utcnow() + timedelta(hours=TOKEN_EXPIRE_HOURS)
    return jwt.encode(payload, SECRET_KEY, algorithm=ALGORITHM)


def verify_token(token: str) -> dict:
    try:
        return jwt.decode(token, SECRET_KEY, algorithms=[ALGORITHM])
    except JWTError:
        raise HTTPException(status_code=401, detail="Invalid token")


def get_current_user(
    creds: HTTPAuthorizationCredentials = Depends(security),
    db: Session = Depends(get_db),
):
    """Extract the authenticated user from the JWT bearer token."""
    if not creds:
        # Dev fallback: return admin user
        user = db.query(User).filter(User.id == "admin1").first()
        if user:
            return user
        raise HTTPException(status_code=401, detail="Not authenticated")

    payload = verify_token(creds.credentials)
    user_id = payload.get("sub")
    role = payload.get("role")

    # Check users table first, then operators
    if role in ("soc_operator", "soc_supervisor"):
        op = db.query(Operator).filter(Operator.id == user_id).first()
        if op:
            return op
    else:
        user = db.query(User).filter(User.id == user_id).first()
        if user:
            return user

    raise HTTPException(status_code=401, detail="User not found")
