ARG GO_IMAGE=golang:1.27.4
ARG NODE_IMAGE=node:24-bookworm-slim
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot

FROM ${NODE_IMAGE} AS frontend
WORKDIR /source/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts
COPY web/index.html web/tsconfig.json web/vite.config.ts web/orval.config.ts web/openapi.json ./
COPY web/src/ ./src/
COPY web/public/ ./public/
RUN npm run build

FROM ${GO_IMAGE} AS backend
WORKDIR /source
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY cmd/safeagent/ ./cmd/safeagent/
COPY internal/ ./internal/
COPY web/*.go ./web/
COPY --from=frontend /source/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -tags embedded -o /safeagent ./cmd/safeagent

FROM ${RUNTIME_IMAGE}
LABEL org.opencontainers.image.licenses="Apache-2.0"
WORKDIR /app
COPY --from=backend /safeagent /app/safeagent
COPY LICENSE NOTICE /app/
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/safeagent", "-config", "/app/config.toml"]
CMD ["serve"]
