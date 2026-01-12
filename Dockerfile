FROM --platform=$BUILDPLATFORM golang:1.24.4 AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /app/midway .

FROM gcr.io/distroless/base-debian11

WORKDIR /

# Create cache directory
VOLUME ["/var/cache/midway"]
ENV CACHE_DIR="/var/cache/midway"

COPY --from=builder /app/midway .

EXPOSE 8900

CMD ["./midway"]
