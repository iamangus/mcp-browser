FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /mcp-browser ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache \
    chromium \
    nss \
    freetype \
    harfbuzz \
    ca-certificates \
    ttf-freefont \
    dbus \
    xvfb \
    xauth \
    && rm -rf /var/cache/apk/*

ENV CHROMIUM_PATH=/usr/bin/chromium-browser
ENV PATH="/usr/bin:${PATH}"

COPY --from=builder /mcp-browser /usr/local/bin/mcp-browser
COPY docker-entrypoint.sh /docker-entrypoint.sh

RUN adduser -D -s /bin/sh appuser \
    && chmod +x /docker-entrypoint.sh
USER appuser

EXPOSE 3000

ENTRYPOINT ["/docker-entrypoint.sh"]
