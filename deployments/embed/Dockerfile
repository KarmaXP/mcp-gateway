# ONNX embedding service (all-MiniLM-L6-v2 → ONNX at build time; FastAPI at runtime).
# Stages: model-builder bakes /model-cache; runtime copies cache and runs uvicorn.

# ---- Stage 1: download + export model to ONNX ----
FROM python:3.12-slim AS model-builder

ENV HF_HOME=/model-cache \
    TRANSFORMERS_OFFLINE=0 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    python3-dev \
    libgomp1 \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir --upgrade pip setuptools wheel

# CPU-only torch first: default torch wheels pull CUDA deps (multi-GB) and OOM typical CI runners.
RUN pip install --no-cache-dir torch --index-url https://download.pytorch.org/whl/cpu

RUN pip install --no-cache-dir sentence-transformers==3.3.1

RUN pip install --no-cache-dir "optimum[onnxruntime]==1.23.0"

RUN python -c "\
from sentence_transformers import SentenceTransformer; \
m = SentenceTransformer('sentence-transformers/all-MiniLM-L6-v2', backend='onnx'); \
v = m.encode(['warmup'], normalize_embeddings=True); \
assert v.shape == (1, 384), f'unexpected shape {v.shape}'; \
print(f'[embed-builder] OK  model=all-MiniLM-L6-v2  dim={v.shape[1]}'); \
"

# ---- Stage 2: runtime ----
FROM python:3.12-slim AS runtime

ENV HF_HOME=/model-cache \
    TRANSFORMERS_OFFLINE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    python3-dev \
    libgomp1 \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir --upgrade pip setuptools wheel

# CPU-only torch first: default torch wheels pull CUDA deps (multi-GB) and OOM typical CI runners.
RUN pip install --no-cache-dir torch --index-url https://download.pytorch.org/whl/cpu

RUN pip install --no-cache-dir sentence-transformers==3.3.1

RUN pip install --no-cache-dir "optimum[onnxruntime]==1.23.0"

RUN pip install --no-cache-dir fastapi==0.115.6 "uvicorn[standard]==0.34.0"

COPY --from=model-builder /model-cache /model-cache

COPY server.py .

RUN addgroup --system embed && adduser --system --ingroup embed embed
USER embed

EXPOSE 8001

HEALTHCHECK --interval=15s --timeout=5s --start-period=60s --retries=5 \
  CMD python -c "import urllib.request; urllib.request.urlopen('http://localhost:8001/healthz').read()"

CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8001", "--workers", "1"]
