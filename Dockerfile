FROM docker.m.daocloud.io/library/golang:1.26-alpine AS builder

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/browser-automation-service ./cmd/browser-automation-service

FROM docker.m.daocloud.io/library/python:3.12-slim-bookworm

ARG CAMOUFOX_FETCH_PROXY=""

RUN sed -i 's/deb.debian.org/mirrors.ustc.edu.cn/g' /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        fonts-noto-cjk \
        git \
        libasound2 \
        libatk-bridge2.0-0 \
        libatk1.0-0 \
        libcairo2 \
        libcups2 \
        libdbus-1-3 \
        libdbus-glib-1-2 \
        libdrm2 \
        libgbm1 \
        libglib2.0-0 \
        libgtk-3-0 \
        libnspr4 \
        libnss3 \
        libpango-1.0-0 \
        libx11-6 \
        libx11-xcb1 \
        libxcb1 \
        libxcomposite1 \
        libxdamage1 \
        libxext6 \
        libxfixes3 \
        libxkbcommon0 \
        libxrandr2 \
        libxt6 \
        tzdata \
        wget \
        xauth \
        xdg-utils \
        xvfb \
    && rm -rf /var/lib/apt/lists/*

RUN pip config set global.index-url https://pypi.tuna.tsinghua.edu.cn/simple \
    && pip install --no-cache-dir 'camoufox[geoip]' playwright \
    && if [ -n "$CAMOUFOX_FETCH_PROXY" ]; then \
         HTTP_PROXY="$CAMOUFOX_FETCH_PROXY" HTTPS_PROXY="$CAMOUFOX_FETCH_PROXY" python -m camoufox fetch; \
       else \
         python -m camoufox fetch; \
       fi

WORKDIR /app
COPY --from=builder /out/browser-automation-service /usr/local/bin/browser-automation-service
COPY migrations ./migrations

ENV BROWSER_AUTOMATION_LISTEN_ADDR=:50051 \
    BROWSER_AUTOMATION_RUNTIME=camoufox \
    BROWSER_AUTOMATION_MIGRATIONS_DIR=/app/migrations \
    BROWSER_AUTOMATION_ARTIFACTS_DIR=/tmp/browser-automation-artifacts

EXPOSE 50051
CMD ["browser-automation-service"]
