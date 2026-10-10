# syntax=docker/dockerfile:1
# Go API image: the api, migrate, seed and admin binaries on a minimal non-root base.
# Built by deploy/docker-compose.yml (context: backend/).

FROM golang:1.26.9-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate \
 && go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed \
 && go build -trimpath -ldflags="-s -w" -o /out/admin ./cmd/admin \
 && mkdir -p /out/data/uploads

# distroless/static: CA certificates and time zones, no shell, runs as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/api /out/migrate /out/seed /out/admin /app/
# Upload folder owned by the runtime user; a new named volume mounted here copies this ownership.
COPY --from=build --chown=nonroot:nonroot /out/data /data
USER nonroot
EXPOSE 8085
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 CMD ["/app/api", "healthcheck"]
CMD ["/app/api"]
