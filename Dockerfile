FROM golang:1.24.4 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/midway .

FROM gcr.io/distroless/base-debian11

WORKDIR /

# Create cache directory
VOLUME ["/var/cache/midway"]
ENV CACHE_DIR="/var/cache/midway"

COPY --from=builder /app/midway .

EXPOSE 8900

CMD ["./midway"]
