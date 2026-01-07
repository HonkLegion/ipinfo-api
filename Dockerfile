# ---- build stage ----
FROM golang:1.25-alpine AS builder

WORKDIR /app

# kvůli HTTPS
RUN apk add --no-cache ca-certificates

COPY go.mod go.sum app.go ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ipinfo-api ./app.go

# ---- runtime stage ----
FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY --from=builder /app/ipinfo-api /app/ipinfo-api
COPY config.yaml /app/config.yaml

USER nonroot:nonroot

EXPOSE 9000

ENTRYPOINT ["/app/ipinfo-api"]
