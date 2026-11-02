# Copyright The Phorepoc Authors
# SPDX-License-Identifier: Apache-2.0

# PhoneLab for Linux hosts (build context = the repository root):
#   docker build -f docker/Dockerfile -t phonelab .
# Released images: ghcr.io/phorepoc/phonelab:<version> (linux/amd64, linux/arm64; .github/workflows/release.yml).
# The web UI is built in the first stage and embedded into the static Go binary. Both build stages run on the build
# machine's own platform and Go cross-compiles for the target, so a multi-arch build needs no emulation.
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS server
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=web /src/web/embed/dist ./web/embed/dist
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
      -o /phonelab ./cmd/phonelab \
 && GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
      -o /phe ./cmd/phe

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=server /phonelab /phe /usr/local/bin/
COPY phonelab.toml /etc/phonelab/phonelab.toml
ENV PHONELAB_DATA=/data
VOLUME /data
WORKDIR /work
EXPOSE 8737
ENTRYPOINT ["phonelab", "-config", "/etc/phonelab/phonelab.toml"]
