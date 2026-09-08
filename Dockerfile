FROM oven/bun:1.4.2 AS frontend
WORKDIR /src
COPY package.json bun.lock ./
COPY .env.example .env
COPY readest readest
COPY frontend-overrides frontend-overrides
COPY scripts scripts
RUN bun run prepare:frontend
RUN bun --cwd .build/readest/apps/readest-app run build-personal

FROM golang:1.27 AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY server server
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /readest ./server/cmd/readest

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=backend /readest /app/readest
COPY --from=frontend /src/.build/readest/apps/readest-app/out /app/web
ENV DATA_DIR=/data PORT=3000 STATIC_DIR=/app/web
EXPOSE 3000
VOLUME ["/data"]
USER nonroot:nonroot
ENTRYPOINT ["/app/readest"]
