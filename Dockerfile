# syntax=docker/dockerfile:1

FROM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
ARG ASTER_VERSION=0.1.0-dev
ARG ASTER_COMMIT=unknown
ARG ASTER_DATE=unknown
ARG ASTER_BUILD_TYPE=source
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X github.com/astercloud/asterrouter/backend/internal/buildinfo.Version=${ASTER_VERSION} -X github.com/astercloud/asterrouter/backend/internal/buildinfo.Commit=${ASTER_COMMIT} -X github.com/astercloud/asterrouter/backend/internal/buildinfo.Date=${ASTER_DATE} -X github.com/astercloud/asterrouter/backend/internal/buildinfo.BuildType=${ASTER_BUILD_TYPE}" \
    -o /out/asterrouter ./cmd/asterrouter

FROM alpine:3.22
WORKDIR /app
RUN adduser -D -H -u 10001 asterrouter
COPY --from=backend /out/asterrouter /app/asterrouter
COPY --from=frontend /src/frontend/dist /app/frontend/dist
ENV ASTERROUTER_SERVER_HTTP_LISTEN=:8080 \
    ASTERROUTER_SERVER_HTTP_FRONTEND_DIR=/app/frontend/dist
EXPOSE 8080
USER asterrouter
ENTRYPOINT ["/app/asterrouter"]
CMD ["server"]
