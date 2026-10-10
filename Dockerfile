ARG GO_IMAGE=golang:1.27.1-alpine
ARG RUNTIME_IMAGE=alpine:3.23
FROM ${GO_IMAGE} AS build
WORKDIR /src
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /traffic-manager-lite ./cmd/traffic-manager-lite
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tml-traffic-audit ./cmd/tml-traffic-audit
FROM build AS test
RUN apk add --no-cache build-base
RUN CGO_ENABLED=1 go test -race ./... && go vet ./...
FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates tzdata && addgroup -g 10001 tml && adduser -D -u 10001 -G tml tml && mkdir -p /app/data /app/backups /app/configs && chown -R tml:tml /app
WORKDIR /app
COPY --from=build /traffic-manager-lite /app/traffic-manager-lite
COPY --from=build /tml-traffic-audit /app/tml-traffic-audit
USER 10001:10001
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/app/traffic-manager-lite", "healthcheck"]
ENTRYPOINT ["/app/traffic-manager-lite"]
