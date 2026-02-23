# ── Stage 1: build the React UI ──────────────────────────────
FROM node:20-alpine AS ui-builder

WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --prefer-offline

COPY web/ ./
# Override outDir to /ui-dist (vite.config.ts default targets ../internal/api/static)
RUN npm run build -- --outDir /ui-dist

# ── Stage 2: build the Go binary ─────────────────────────────
FROM golang:1.24-alpine AS go-builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Overlay the built UI
COPY --from=ui-builder /ui-dist ./internal/api/static/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /out/bit-rot-detector \
    ./cmd/bit-rot-detector

# ── Stage 3: minimal runtime image ───────────────────────────
FROM scratch

COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=go-builder /out/bit-rot-detector /usr/local/bin/bit-rot-detector

USER 65534:65534

ENTRYPOINT ["/usr/local/bin/bit-rot-detector"]
