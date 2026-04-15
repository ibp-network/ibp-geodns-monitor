FROM golang:1.24-alpine AS builder

ARG VERSION=dev
ARG BUILD_TIME=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -ldflags "-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
	-o /out/ibp-monitor ./src/IBPMonitor.go

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /opt/ibp-geodns-monitor

COPY --from=builder /out/ibp-monitor /usr/local/bin/ibp-monitor

ENTRYPOINT ["/usr/local/bin/ibp-monitor"]
CMD ["-config", "/opt/ibp-geodns-monitor/config/ibpmonitor.json"]
