# syntax=docker/dockerfile:1.7
FROM docker.io/oven/bun:1.3.14-alpine AS assets
WORKDIR /src
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY assets ./assets
COPY scripts ./scripts
COPY internal/web/templates ./internal/web/templates
RUN bun run build

FROM docker.io/library/golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=assets /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bookshelf ./cmd/bookshelf
# Staged empty tree so the runtime data directory exists and is owned by the
# unprivileged runtime user; distroless has no shell to mkdir it later.
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bookshelf /app/bookshelf
COPY --from=build --chown=65532:65532 /out/data /app/data
ENV DATA_DIR=/app/data \
    PORT=8070
USER 65532:65532
VOLUME ["/app/data"]
EXPOSE 8070
ENTRYPOINT ["/app/bookshelf"]
CMD ["serve"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD ["/app/bookshelf", "healthcheck"]
