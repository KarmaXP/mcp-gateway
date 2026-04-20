# =============================================================================
# MCP Gateway — Multi-stage Docker build
# =============================================================================

# ---- Stage 1: Build ----
# Use the floating alpine tag so the image resolves regardless of which Alpine
# version ships with Go 1.26 (currently 3.22; avoid pinning the alpine minor
# here since CGO_ENABLED=0 produces a fully static binary anyway).
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Download dependencies first for layer caching.
# go.sum may not exist yet in early development (no deps); handled below.
COPY go.mod .
RUN test -f go.sum && cp go.sum . || true
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /bin/mcp-gateway \
      ./cmd/gateway

# ---- Stage 2: Runtime ----
# Static binary has no libc dependency; any recent Alpine works.
FROM alpine:3.22 AS runtime

# ca-certificates: needed for outbound TLS (JWKS, OIDC, embedding APIs)
# tzdata: for correct time zone handling in logs
RUN apk add --no-cache ca-certificates tzdata

# Run as non-root
RUN addgroup -S mcp && adduser -S mcp -G mcp

COPY --from=builder /bin/mcp-gateway /bin/mcp-gateway

USER mcp

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/healthz && wget -qO- http://localhost:8080/readyz || exit 1

ENTRYPOINT ["/bin/mcp-gateway"]
