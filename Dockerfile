FROM node:22-alpine AS web
WORKDIR /build/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26.4-alpine AS backend
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /relay ./cmd/relay
RUN mkdir -p /relay-data && chmod 700 /relay-data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /relay /app/relay
COPY --from=backend --chown=65532:65532 /relay-data /var/lib/relay
COPY --from=web /build/web/dist /app/web
ENV RELAY_LISTEN=:8080 RELAY_WEB_DIR=/app/web RELAY_DATA_DIR=/var/lib/relay
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/app/relay"]
CMD ["serve"]
