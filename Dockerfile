# Build stage - use official Karpenter approach
FROM public.ecr.aws/docker/library/golang:1.22-alpine AS builder

# Install dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /workspace

# Copy all source code first
COPY . .

# Download dependencies
RUN go mod download

# Build the controller
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