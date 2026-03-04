# ---- build stage ----
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

WORKDIR /app

ARG TARGETOS
ARG TARGETARCH

# kvůli HTTPS
RUN apk add --no-cache ca-certificates

COPY go.mod go.sum app.go ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o ipinfo-api ./app.go

# ---- runtime stage ----
FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /app

COPY --from=builder /app/ipinfo-api /app/ipinfo-api
COPY config.yaml /app/config.yaml

EXPOSE 9000

ENTRYPOINT ["/app/ipinfo-api"]
