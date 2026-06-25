FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY main.go .
RUN go mod init canary-demo-app && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
ARG VERSION=unknown
ENV APP_VERSION=${VERSION}
ENV PORT=8080
COPY --from=builder /app /app
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/app"]
