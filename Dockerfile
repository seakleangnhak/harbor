ARG SOURCE_DATE_EPOCH=0
ARG HARBOR_VERSION=1.2.0
FROM --platform=$BUILDPLATFORM node:24-bookworm-slim@sha256:a5f4366e834695d5f363b920c104953a535d3a9864d1bdf74566a0ebe01daee5 AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.2-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS server-source
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal

FROM server-source AS backend-checks
RUN apt-get update && apt-get install -y --no-install-recommends python3 && rm -rf /var/lib/apt/lists/*
COPY scripts ./scripts

FROM server-source AS server
ARG GO_TAGS=telegram
ARG HARBOR_VERSION
ARG TARGETOS
ARG TARGETARCH
RUN printf '%s\n' "$HARBOR_VERSION" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$' && \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" GOGC=40 go build -p 1 -tags "$GO_TAGS" -trimpath -buildvcs=false -ldflags="-s -w -X main.buildVersion=$HARBOR_VERSION" -o /drive ./cmd/drive
RUN CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" GOGC=40 go build -p 1 -trimpath -buildvcs=false -ldflags="-s -w" -o /harbor-recovery ./cmd/harbor-recovery
RUN mkdir -p /runtime/data && chmod 0700 /runtime/data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG HARBOR_VERSION
LABEL org.opencontainers.image.version="$HARBOR_VERSION" \
      org.opencontainers.image.description="Private, self-hosted Telegram-backed cloud drive" \
      org.opencontainers.image.licenses="MIT"
WORKDIR /app
COPY LICENSE THIRD_PARTY_NOTICES.md /app/
COPY --from=server /drive /app/drive
COPY --from=server /harbor-recovery /app/harbor-recovery
COPY --from=web /src/web/dist /app/web/dist
COPY --from=server --chown=65532:65532 /runtime/ /
ENV DRIVE_ADDR=0.0.0.0:8080 DRIVE_DATA_DIR=/data DRIVE_STATIC_DIR=/app/web/dist DRIVE_ENV_FILE=- GOMEMLIMIT=512MiB
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=15s --retries=3 CMD ["/app/drive", "health"]
ENTRYPOINT ["/app/drive"]
