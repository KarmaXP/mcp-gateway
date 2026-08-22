FROM golang:1.26.7-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /mock_upstream ./scripts/mock_upstream

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /mock_upstream /usr/local/bin/mock_upstream
ENTRYPOINT ["/usr/local/bin/mock_upstream"]
