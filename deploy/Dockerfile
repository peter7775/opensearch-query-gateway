FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /gateway ./cmd/gateway

FROM alpine:3.20
COPY --from=build /gateway /usr/local/bin/gateway
COPY configs/config.yaml /etc/gateway/config.yaml
COPY internal/rules/bootstrap.pl /etc/gateway/bootstrap.pl
ENV GATEWAY_CONFIG=/etc/gateway/config.yaml
ENTRYPOINT ["/usr/local/bin/gateway"]
