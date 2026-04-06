"""
ONNX Embedding Service — all-MiniLM-L6-v2
==========================================
Wraps the sentence-transformers ONNX backend in a minimal FastAPI server.

Design notes
------------
- model.encode() is CPU-bound; it runs inside a ThreadPoolExecutor so it
  never blocks uvicorn's async event loop.
- A single worker (--workers 1 in CMD) avoids loading the ONNX session
  multiple times in the same process.
- normalize_embeddings=True produces unit-norm vectors consistent with
  Qdrant Cosine collections (cosine ≡ dot product on unit vectors, faster).
- Batch requests are accepted to amortise tokenization overhead.
"""

import asyncio
import logging
import os
import time
from concurrent.futures import ThreadPoolExecutor
from typing import List

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field
from sentence_transformers import SentenceTransformer

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------
logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger("embed")

# ---------------------------------------------------------------------------
# Model — loaded once at startup from the baked cache
# ---------------------------------------------------------------------------
MODEL_ID = "sentence-transformers/all-MiniLM-L6-v2"
DIMENSIONS = 384

log.info("Loading ONNX model: %s", MODEL_ID)
_t0 = time.perf_counter()
model = SentenceTransformer(MODEL_ID, backend="onnx")
log.info("Model ready in %.2fs  dim=%d", time.perf_counter() - _t0, DIMENSIONS)

# CPU-bound executor: keep it single-threaded to match --workers 1
_executor = ThreadPoolExecutor(max_workers=1, thread_name_prefix="onnx")

# ---------------------------------------------------------------------------
# FastAPI app
# ---------------------------------------------------------------------------
app = FastAPI(
    title="MCP Gateway — Embedding Service",
    description="Local ONNX inference for all-MiniLM-L6-v2 (384 dims, Cosine)",
    version="1.0.0",
)


class EmbedRequest(BaseModel):
    texts: List[str] = Field(..., min_length=1, max_length=256,
                             description="List of texts to embed (max 256 per request)")


class EmbedResponse(BaseModel):
    embeddings: List[List[float]]
    model: str
    dimensions: int
    latency_ms: float


@app.post("/embed", response_model=EmbedResponse)
async def embed(req: EmbedRequest) -> EmbedResponse:
    """Encode a batch of texts and return L2-normalised vectors (384-dim)."""
    if not req.texts:
        raise HTTPException(status_code=422, detail="texts must not be empty")

    loop = asyncio.get_event_loop()
    t0 = time.perf_counter()

    # Run blocking ONNX inference off the event loop
    vectors = await loop.run_in_executor(
        _executor,
        lambda: model.encode(req.texts, normalize_embeddings=True).tolist(),
    )

    latency_ms = (time.perf_counter() - t0) * 1000
    log.info("embed  n=%d  latency=%.1fms", len(req.texts), latency_ms)

    return EmbedResponse(
        embeddings=vectors,
        model=MODEL_ID,
        dimensions=DIMENSIONS,
        latency_ms=round(latency_ms, 2),
    )


@app.get("/healthz")
async def healthz():
    """Liveness probe. Returns 200 when the ONNX model is loaded and ready."""
    return {"status": "ok", "model": MODEL_ID, "dimensions": DIMENSIONS}
