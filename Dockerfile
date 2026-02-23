# ── Stage 1: build a fully static binary ────────────────────────────────────
FROM golang:1.24-alpine AS builder

# Install build tools (git is needed for go-generate; ca-certificates for HTTPS)
RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache module downloads separately from source code
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source tree
COPY . .

# Build a fully static binary (CGO_ENABLED=0 → no libc dependency)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" \
    -o /out/bit-rot-detector \
    ./cmd/bit-rot-detector

# ── Stage 2: minimal runtime image ───────────────────────────────────────────
FROM scratch

# TLS root certificates (needed for SMTP)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the static binary
COPY --from=builder /out/bit-rot-detector /usr/local/bin/bit-rot-detector

# Run as a non-root user for security
USER 65534:65534

ENTRYPOINT ["/usr/local/bin/bit-rot-detector"]
