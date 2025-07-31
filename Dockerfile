# Build stage - use latest available Go version
FROM public.ecr.aws/docker/library/golang:1.23-alpine AS builder

# Install dependencies
RUN apk add --no-cache git

# Set Go environment
ENV GO111MODULE=on
ENV GOPROXY=https://proxy.golang.org,direct
ENV GOSUMDB=sum.golang.org
ENV GOTOOLCHAIN=auto

# Set working directory
WORKDIR /workspace

# Copy go.mod and go.sum first
COPY go.mod go.sum ./

# Download dependencies (ignore version requirements)
RUN go mod download || true

# Copy source code
COPY . .

# Build the controller with Go 1.23 (ignore version requirements)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -mod=readonly -ldflags="-s -w" -o karpenter ./cmd/controller/main.go || \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -mod=mod -ldflags="-s -w" -o karpenter ./cmd/controller/main.go

# Runtime stage
FROM gcr.io/distroless/static:nonroot

# Labels
LABEL org.opencontainers.image.source=https://github.com/startappdev/karpenter

# Copy binary
COPY --from=builder /workspace/karpenter /karpenter

# Run as non-root
USER 65532:65532

ENTRYPOINT ["/karpenter"]