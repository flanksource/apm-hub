FROM golang:1.20@sha256:bc5f0b5e43282627279fe5262ae275fecb3d2eae3b33977a7fd200c7a760d6f1 as builder
WORKDIR /app
ARG VERSION
COPY go.mod /app/go.mod
COPY go.sum /app/go.sum
RUN go mod download
COPY ./ ./
WORKDIR /app
RUN go version
RUN make build

FROM ubuntu:jammy@sha256:e6173d4dc55e76b87c4af8db8821b1feae4146dd47341e4d431118c7dd060a74
WORKDIR /app

# install CA certificates
RUN apt-get update && \
  apt-get install -y ca-certificates && \
  rm -Rf /var/lib/apt/lists/*  && \
  rm -Rf /usr/share/doc && rm -Rf /usr/share/man  && \
  apt-get clean

COPY --from=builder /app/.bin/apm-hub /app
ENV ASSUME_NO_MOVING_GC_UNSAFE_RISK_IT_WITH=go1.20
ENTRYPOINT ["/app/apm-hub"]
