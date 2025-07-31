# Build stage - use latest available Go version
FROM public.ecr.aws/docker/library/golang:1.23-alpine AS builder

# Set working directory
WORKDIR /workspace

# Copy just the main.go file first
COPY cmd/controller/main.go ./cmd/controller/main.go

# Build the minimal controller (no dependencies needed)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o karpenter ./cmd/controller/main.go

# Runtime stage
FROM gcr.io/distroless/static:nonroot

# Labels
LABEL org.opencontainers.image.source=https://github.com/startappdev/karpenter

# Copy binary
COPY --from=builder /workspace/karpenter /karpenter

# Run as non-root
USER 65532:65532

ENTRYPOINT ["/karpenter"]