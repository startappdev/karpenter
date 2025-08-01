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
    go build -ldflags="-s -w" \
    -o karpenter ./cmd/controller/...

# Runtime stage
FROM gcr.io/distroless/static:nonroot

# Copy the binary
COPY --from=builder /workspace/karpenter /karpenter

# Expose port
EXPOSE 8080

# Set the entrypoint
ENTRYPOINT ["/karpenter"]