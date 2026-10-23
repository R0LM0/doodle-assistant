# syntax=docker/dockerfile:1

# --- 1. UI: build the Vite bundle -------------------------------------------
FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# --- 2. Go binary ------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
# Static binary, no cgo: the runtime image needs nothing but the file.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/assistant ./cmd/assistant

# --- 3. Runtime --------------------------------------------------------------
# distroless/static: no shell, no package manager, non-root by default. An
# assistant exposed over HTTP does not need a shell to answer questions.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=build /out/assistant /app/assistant
COPY --from=build /src/web/dist /app/web/dist
COPY --from=build /src/docs /app/docs
COPY --from=build /src/migrations /app/migrations

ENV HTTP_ADDR=:8080 \
    WEB_DIR=/app/web/dist \
    DOCS_DIR=/app/docs \
    LOG_LEVEL=info

USER nonroot:nonroot
EXPOSE 8080

# `migrate` then `serve`: run the migration as a one-off container before the
# first start, e.g. docker compose run --rm app migrate
ENTRYPOINT ["/app/assistant"]
CMD ["serve"]
