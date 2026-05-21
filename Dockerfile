ARG BROWSER_AUTOMATION_RUNTIME_IMAGE=byte-v-forge/browser-automation-runtime:camoufox-0.4.11-playwright-1.59.0-py3.12-bookworm

FROM docker.m.daocloud.io/library/golang:1.26-alpine AS builder

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/browser-automation-service ./cmd/browser-automation-service

FROM ${BROWSER_AUTOMATION_RUNTIME_IMAGE}

WORKDIR /app
COPY --from=builder /out/browser-automation-service /usr/local/bin/browser-automation-service
COPY migrations ./migrations

ENV BROWSER_AUTOMATION_LISTEN_ADDR=:50051 \
    BROWSER_AUTOMATION_RUNTIME=camoufox \
    BROWSER_AUTOMATION_MIGRATIONS_DIR=/app/migrations \
    BROWSER_AUTOMATION_ARTIFACTS_DIR=/tmp/browser-automation-artifacts

EXPOSE 50051
CMD ["browser-automation-service"]
