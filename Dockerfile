# ============================================================================
# TODO (User Practice): Phase 13.1 - Task 3: Multi-Stage Production Dockerfile
# ----------------------------------------------------------------------------
# Objective:
#   Build a minimal, secure, production-ready container image for the
#   DAMASCUS control plane using Docker multi-stage builds.
#
# Stage 1 (Builder):
#   - Base: golang:alpine
#   - Set WORKDIR /app
#   - Cache Go module dependencies (go.mod, go.sum -> go mod download)
#   - Copy source code
#   - Build statically linked binary with CGO_ENABLED=0 and stripped debug symbols:
#     RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/damascus ./cmd/damascus
#
# Stage 2 (Final Runtime):
#   - Base: alpine:latest
#   - Install ca-certificates and tzdata
#   - Create non-root unprivileged user (UID 10001) for security
#   - Copy compiled binary from builder stage
#   - Switch to non-root user
#   - Expose port 8080
#   - Set ENTRYPOINT ["/bin/damascus"]
# ============================================================================

# >>> YOUR CODE FOR TASK 3 STARTS HERE <<<

# Stage 1: Build static Go binary
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install build-time requirements
RUN apk add --no-cache git ca-certificates tzdata

# Cache module dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source tree and compile
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/damascus ./cmd/damascus

# Stage 2: Minimal runtime image
FROM alpine:3.20

# Install runtime security certificates and timezone support
RUN apk --no-cache add ca-certificates tzdata \
    && addgroup -g 10001 damascus \
    && adduser -D -u 10001 -G damascus damascus

WORKDIR /app

# Copy binary and migrations from builder
COPY --from=builder /bin/damascus /bin/damascus
COPY --from=builder /app/migrations /app/migrations

# Run as unprivileged user
USER damascus:damascus

EXPOSE 8080

ENTRYPOINT ["/bin/damascus"]

# >>> YOUR CODE FOR TASK 3 ENDS HERE <<<
