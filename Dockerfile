FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o /encli ./cmd/encli/

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /encli /usr/local/bin/encli
ENTRYPOINT ["encli"]
