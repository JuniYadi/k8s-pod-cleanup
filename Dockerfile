# Build stage.
#
# Pinned to the *builder's* architecture, not the target's. The binary is
# CGO-free, so Go cross-compiles it directly: the arm64 image is produced
# natively on an amd64 runner instead of running the whole Go toolchain under
# QEMU emulation, which took ~10 minutes per release.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

WORKDIR /app

# Copy dependency definition
COPY src/go.mod src/go.sum ./
RUN go mod download

# Copy source code
COPY src/ ./

# Build statically linked binary for the requested target architecture.
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="-w -s" -o /app/bin/k8s-pod-cleanup ./cmd/cleaner

# Minimal runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=builder /app/bin/k8s-pod-cleanup /usr/local/bin/k8s-pod-cleanup

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/k8s-pod-cleanup"]
