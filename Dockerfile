ARG GO_VERSION=1.16
ARG GOLANGCI_LINT_VERSION=v1.39.0

FROM golangci/golangci-lint:${GOLANGCI_LINT_VERSION} AS lint-base

FROM golang:${GO_VERSION} AS builder

COPY --from=lint-base /usr/bin/golangci-lint /usr/bin/golangci-lint

COPY . /app

WORKDIR /app

RUN make

FROM debian:9-slim

COPY --from=builder /app/bin/linux/amd64/kubexit /app/bin/kubexit

CMD ["/app/bin/kubexit"]