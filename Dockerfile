# Build the manager binary
# syntax=docker/dockerfile:experimental
ARG GOLANG_VERSION=golang:1.17.6
FROM --platform=${BUILDPLATFORM} ${GOLANG_VERSION} as builder
WORKDIR /workspace

# Run this with docker build --build_arg $(go env GOPROXY) to override the goproxy
ARG goproxy=https://goproxy.io,direct
ENV GOPROXY=${goproxy}

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Build
ARG TARGETOS
ARG TARGETARCH
ARG ldflags
RUN --mount=type=bind,target=. \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -a -ldflags "${ldflags} -extldflags '-static'" \
    -o /out/manager .

# Copy the controller-manager into a thin image
ARG TARGETPLATFORM
FROM --platform=${TARGETPLATFORM} gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /out/manager .
# Use uid of nonroot user (65532) because kubernetes expects numeric user when applying PSPs
USER 65532
ENTRYPOINT ["/manager"]