# PhoneLab for Linux hosts (build context = the repository root):
#   docker build -f docker/Dockerfile -t phonelab .
# The web UI is built in the first stage and embedded into the static Go binary.
FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=web /src/web/embed/dist ./web/embed/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /phonelab ./cmd/phonelab \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /ph ./cmd/ph

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=server /phonelab /ph /usr/local/bin/
COPY phonelab.toml /etc/phonelab/phonelab.toml
ENV PHONELAB_DATA=/data
VOLUME /data
WORKDIR /work
EXPOSE 8737
ENTRYPOINT ["phonelab", "-config", "/etc/phonelab/phonelab.toml"]
