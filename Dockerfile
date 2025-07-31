# Build stage
FROM public.ecr.aws/docker/library/golang:1.24-alpine AS builder

# Install required packages
RUN apk add --no-cache git ca-certificates

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.mod
COPY go.sum go.sum

# Download dependencies
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY pkg/ pkg/

# Build the controller binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w -X main.version=${VERSION:-dev}" \
    -o karpenter ./cmd/controller/main.go

# Runtime stage
FROM gcr.io/distroless/static:nonroot

# Labels
LABEL org.opencontainers.image.source=https://github.com/startappdev/karpenter

# Copy binary from builder
COPY --from=builder /workspace/karpenter /karpenter

# Run as non-root
USER 65532:65532

# Expose ports
EXPOSE 8080 8443 8001

ENTRYPOINT ["/karpenter"]