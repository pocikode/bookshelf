# syntax=docker/dockerfile:1.7
FROM oven/bun:1.3.14-alpine AS assets
WORKDIR /src
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY assets ./assets
COPY scripts ./scripts
COPY internal/web/templates ./internal/web/templates
RUN bun run build

FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=assets /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /app ./cmd/bookshelf

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app /app
USER 65532:65532
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app"]
CMD ["serve"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD ["/app", "healthcheck"]
