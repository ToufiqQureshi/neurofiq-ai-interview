import os
from fastapi import Header, HTTPException

INTERNAL_SECRET = os.getenv("INTERNAL_SECRET")

def verify_internal_secret(x_internal_secret: str = Header(None)):
    # Fail closed: if INTERNAL_SECRET isn't configured, reject every request
    # instead of falling back to a source-visible default.
    if not INTERNAL_SECRET or x_internal_secret != INTERNAL_SECRET:
        raise HTTPException(status_code=403, detail="forbidden")
