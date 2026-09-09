from fastapi import FastAPI
import threading
import grpc_server
from feature_interview import router as interview_router
from feature_discovery import router as discovery_router

app = FastAPI()

@app.on_event("startup")
def startup_event():
    # Start the gRPC server in a daemon thread so it runs alongside FastAPI
    threading.Thread(target=grpc_server.serve, daemon=True).start()

@app.get("/internal/health")
async def health_check():
    return {"status": "healthy"}

# Include the modular routers
app.include_router(interview_router, prefix="/internal")
app.include_router(discovery_router, prefix="/internal")
