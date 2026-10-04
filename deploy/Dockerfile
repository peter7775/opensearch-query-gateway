FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gateway ./cmd/gateway \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/schema-export ./cmd/schema-export

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget && adduser -D -H -u 10001 gateway
COPY --from=build /out/gateway /usr/local/bin/gateway
COPY --from=build /out/schema-export /usr/local/bin/schema-export
COPY configs/config.yaml /etc/gateway/config.yaml
COPY internal/rules/bootstrap.pl /etc/gateway/bootstrap.pl
ENV GATEWAY_CONFIG=/etc/gateway/config.yaml \
    GATEWAY_RULES_BOOTSTRAP_FILE=/etc/gateway/bootstrap.pl
USER gateway
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=3 CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/gateway"]
