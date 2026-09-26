# syntax=docker/dockerfile:1

FROM golang:1.27-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go install github.com/a-h/templ/cmd/templ@latest \
  && curl -sL -o /usr/local/bin/tailwindcss \
    https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/tailwindcss-linux-x64 \
  && chmod +x /usr/local/bin/tailwindcss \
  && templ generate \
  && tailwindcss --input web/static/css/input.css --output web/static/css/app.css --minify \
  && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kurachat ./cmd/kurachat

FROM debian:12-slim
# LibreOffice renders docx/pptx uploads to PDF (headless, per-file temp
# profile). Writer + Impress only; ~350 MB, the price of faithful office
# conversion. DejaVu covers rendering when uploads lack embedded fonts.
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
    libreoffice-writer libreoffice-impress fonts-dejavu \
  && rm -rf /var/lib/apt/lists/* \
  && useradd -u 1000 -m app
USER app
WORKDIR /app

COPY --from=build /out/kurachat /app/kurachat
COPY --from=build /src/web/static /app/web/static

ENV DATA_DIR=/data
ENV BIND=0.0.0.0:80
VOLUME /data
EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD curl -fsS http://127.0.0.1/up || exit 1

CMD ["./kurachat", "serve"]
