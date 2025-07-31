# Build stage
FROM golang:1.22-alpine AS builder

# Install required build tools
RUN apk add --no-cache git make ca-certificates

# Set working directory
WORKDIR /workspace

# Copy go mod files and download dependencies
COPY go.mod go.sum ./
# Set Go proxy for better reliability
ENV GOPROXY=https://proxy.golang.org,direct
ENV GOSUMDB=sum.golang.org
# Download dependencies with verbose output for debugging
RUN go mod download -x || (cat go.mod && exit 1)

# Copy source code
COPY . .

# Build the controller with OCI provider support
# Build with specific flags for production
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=${VERSION:-dev}" \
    -a -installsuffix cgo \
    -o karpenter ./kwok/main.go

# Runtime stage - using distroless for minimal attack surface
FROM gcr.io/distroless/static:nonroot

# Labels for container metadata
LABEL org.opencontainers.image.title="Karpenter OCI" \
      org.opencontainers.image.description="Karpenter with OCI provider support for Oracle Kubernetes Engine" \
      org.opencontainers.image.vendor="StartApp" \
      org.opencontainers.image.source="https://github.com/startappdev/karpenter"

# Copy CA certificates for HTTPS connections
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary from builder
COPY --from=builder /workspace/karpenter /karpenter

# Use non-root user
USER 65532:65532

# Expose metrics and webhook ports
EXPOSE 8080 8443 8001

# Set the entrypoint
ENTRYPOINT ["/karpenter"]