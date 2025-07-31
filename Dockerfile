# Build stage - use official Karpenter approach
FROM public.ecr.aws/docker/library/golang:1.22-alpine AS builder

# Install dependencies
RUN apk add --no-cache git

# Set Go environment
ENV GO111MODULE=on
ENV GOPROXY=https://proxy.golang.org,direct
ENV GOSUMDB=sum.golang.org

# Set working directory
WORKDIR /workspace

# Copy go.mod and go.sum first
COPY go.mod go.sum ./

# Debug: Show Go environment and files
RUN go version && \
    echo "=== Files in workspace ===" && \
    ls -la && \
    echo "=== go.mod content (first 10 lines) ===" && \
    head -10 go.mod && \
    echo "=== Attempting go mod download ===" && \
    go mod download -x

# Copy source code
COPY . .

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