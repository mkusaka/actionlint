ARG GOLANG_VER=latest
ARG ALPINE_VER=latest

FROM golang:${GOLANG_VER} AS builder
WORKDIR /go/src/app
COPY go.* *.go ./
COPY cmd cmd/
ENV CGO_ENABLED=0
ARG ACTIONLINT_VER=
RUN go build -v -ldflags "-s -w -X github.com/mkusaka/yactionlint.version=${ACTIONLINT_VER}" ./cmd/yactionlint

FROM koalaman/shellcheck-alpine:stable AS shellcheck

FROM alpine:${ALPINE_VER}
COPY --from=builder /go/src/app/yactionlint /usr/local/bin/
COPY --from=shellcheck /bin/shellcheck /usr/local/bin/shellcheck
RUN apk add --no-cache py3-pyflakes
# Alpine's guest user (UID 405, GID 100); numeric IDs also work without name resolution.
USER 405:100
ENTRYPOINT ["/usr/local/bin/yactionlint"]
