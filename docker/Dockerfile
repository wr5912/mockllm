FROM golang:1.25.1-alpine@sha256:b6ed3fd0452c0e9bcdef5597f29cc1418f61672e9d3a2f55bf02e7222c014abd AS builder

ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
COPY cmd ./cmd
COPY testdata ./testdata
RUN go test ./... \
    && CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/mockllm \
        ./cmd/mockllm

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce

RUN addgroup -S mockllm \
    && adduser -S -D -H -G mockllm mockllm

COPY --from=builder --chown=mockllm:mockllm /out/mockllm /usr/local/bin/mockllm

USER mockllm:mockllm

EXPOSE 50050

STOPSIGNAL SIGTERM

ENTRYPOINT ["mockllm"]
CMD ["--config", "/etc/mockllm/config.json"]
