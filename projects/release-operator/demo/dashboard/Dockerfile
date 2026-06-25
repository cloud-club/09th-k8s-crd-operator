FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api api
COPY demo/dashboard demo/dashboard
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app ./demo/dashboard

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
ENV PORT=8080
COPY --from=builder /app /app
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/app"]
