ARG BASE_IMAGE
# Build the manager binary
FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.19-alpine as builder
ARG GOPROXY
ENV GOPROXY=${GOPROXY:-https://goproxy.cn}
WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY version/ version/

# Build
ARG TARGETARCH
ARG VERSION
ARG GITVERSION
RUN GO111MODULE=on CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -a -ldflags "-s -w -X github.com/kubevela/workflow/version.VelaVersion=${VERSION:-undefined} -X github.com/kubevela/workflow/version.GitRevision=${GITVERSION:-undefined}" \
    -o vela-workflow-${TARGETARCH} cmd/main.go

FROM ${BASE_IMAGE:-alpine:3.15}
# This is required by daemon connecting with cri
RUN apk add --no-cache ca-certificates bash expat

WORKDIR /

ARG TARGETARCH
COPY --from=builder /workspace/vela-workflow-${TARGETARCH} /usr/local/bin/vela-workflow

ENTRYPOINT ["vela-workflow"]
