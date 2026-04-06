# =============================================================================
# ONNX Embedding Service — all-MiniLM-L6-v2, 384 dimensions
#
# Two-stage build:
#   model-builder : downloads and converts all-MiniLM-L6-v2 to ONNX at build
#                   time so the final image needs no internet access at runtime.
#   runtime       : minimal image with the baked model + FastAPI server.
#
# API:
#   POST /embed    {"texts": ["..."]}
#                  → {"embeddings": [[...]], "model": "all-MiniLM-L6-v2", "dimensions": 384}
#   GET  /healthz  → {"status": "ok"}
# =============================================================================

# ---- Stage 1: download + export model to ONNX ----
FROM python:3.12-slim AS model-builder

ENV HF_HOME=/model-cache \
    TRANSFORMERS_OFFLINE=0 \
    PIP_NO_CACHE_DIR=1

RUN apt-get update && apt-get install -y --no-install-recommends curl && rm -rf /var/lib/apt/lists/*

RUN pip install \
    sentence-transformers==3.3.1 \
    "optimum[onnxruntime]==1.23.0"

# Download model and export to ONNX; warm-up verifies the output dimension
RUN python -c "\
from sentence_transformers import SentenceTransformer; \
import numpy as np; \
m = SentenceTransformer('sentence-transformers/all-MiniLM-L6-v2', backend='onnx'); \
v = m.encode(['warmup'], normalize_embeddings=True); \
assert v.shape == (1, 384), f'unexpected shape {v.shape}'; \
print(f'[embed-builder] OK  model=all-MiniLM-L6-v2  dim={v.shape[1]}'); \
"

# ---- Stage 2: minimal runtime image ----
FROM python:3.12-slim AS runtime

ENV HF_HOME=/model-cache \
    TRANSFORMERS_OFFLINE=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

RUN pip install \
    sentence-transformers==3.3.1 \
    "optimum[onnxruntime]==1.23.0" \
    fastapi==0.115.6 \
    "uvicorn[standard]==0.34.0"

# Copy the baked model cache from the builder stage
COPY --from=model-builder /model-cache /model-cache

COPY server.py .

RUN addgroup --system embed && adduser --system --ingroup embed embed
USER embed

EXPOSE 8001

# Health check: attempt a real /healthz request
HEALTHCHECK --interval=15s --timeout=5s --start-period=60s --retries=5 \
  CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8001/healthz').read()"

# Single worker: ONNX runtime is not thread-safe by default with shared sessions
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8001", "--workers", "1"]
