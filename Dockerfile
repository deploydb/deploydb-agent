# syntax=docker/dockerfile:1
# Dockerfile for deploydb-agent

# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /deploydb-agent ./cmd/deploydb-agent

# Runtime stage
FROM alpine:3.19

# Install ca-certificates for HTTPS and postgresql-client for health checks
RUN apk add --no-cache ca-certificates postgresql-client

# Create non-root user
RUN addgroup -g 1000 agent && \
    adduser -u 1000 -G agent -s /bin/sh -D agent

WORKDIR /app

# Copy binary from builder
COPY --from=builder /deploydb-agent /usr/local/bin/deploydb-agent

# Copy config directory
RUN mkdir -p /etc/deploydb && chown -R agent:agent /etc/deploydb

USER agent

ENTRYPOINT ["/usr/local/bin/deploydb-agent"]
CMD ["run", "--config=/etc/deploydb/agent.yaml"]
