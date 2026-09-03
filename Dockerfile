# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy dependency definition
COPY src/go.mod src/go.sum ./
RUN go mod download

# Copy source code
COPY src/ ./

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/bin/k8s-pod-cleanup ./cmd/cleaner

# Minimal runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=builder /app/bin/k8s-pod-cleanup /usr/local/bin/k8s-pod-cleanup

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/k8s-pod-cleanup"]
