# syntax=docker/dockerfile:1
# Nginx image: TLS termination, the admin web (static files) and a reverse proxy
# to the API (REST + WebSocket). Build context: the repository root (see
# deploy/docker-compose.yml); Dockerfile.dockerignore limits what is sent.

FROM node:22-alpine AS web
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
# Same origin as the API: VITE_API_URL stays empty.
RUN npm run build

FROM nginx:1.28-alpine
RUN rm -f /etc/nginx/conf.d/default.conf
# The image renders templates/*.template into conf.d/ with ${SERVER_NAME} filled in at start.
COPY deploy/nginx/templates/ /etc/nginx/templates/
COPY deploy/nginx/snippets/ /etc/nginx/snippets/
COPY --from=web /src/dist /usr/share/nginx/html
