# syntax=docker/dockerfile:1

ARG GO_VERSION=1.25

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/opstack-doctor ./cmd/opstack-doctor

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 65532 opstackdoctor

COPY --from=build /out/opstack-doctor /usr/local/bin/opstack-doctor

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/opstack-doctor"]
