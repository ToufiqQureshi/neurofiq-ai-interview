FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

# We run the gRPC server and the FastAPI server.
# The user might want to run them separately, but for now we'll run the grpc server.
# A proper entrypoint script could run both if needed, but the plan was grpc_server.py.
CMD ["python", "grpc_server.py"]
