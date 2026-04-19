FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o /encli ./cmd/encli/

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 encli
COPY --from=builder /encli /usr/local/bin/encli
USER 10001
ENTRYPOINT ["encli"]
