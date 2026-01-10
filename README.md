# Midway

A high-performance local caching proxy for AWS S3. Midway sits between your application and S3, caching downloaded files on disk to reduce latency and bandwidth costs for frequently accessed objects.

## Features

- **Disk-based LRU Cache**: Automatically evicts least recently used files when the cache reaches its size limit
- **Automatic Region Detection**: Detects S3 bucket regions automatically, no configuration needed
- **Zero Configuration**: Works out of the box with sensible defaults
- **Persistent Cache**: Cache survives restarts by persisting metadata to disk
- **Simple HTTP API**: Request files using a straightforward URL pattern
- **Lightweight**: Single binary with no external dependencies

## Installation

### From Source

```bash
git clone https://github.com/autonoma-ai/midway.git
cd midway
go build -o midway .
```

### Docker

```bash
docker build -t midway .
docker run -p 8900:8900 -v midway-cache:/var/cache/midway midway
```

## Configuration

Midway is configured via environment variables:

| Variable            | Description | Default |
|---------------------|-------------|---------|
| `PORT`              | HTTP server port | `8900` |
| `CACHE_DIR`         | Cache directory path | `~/.cache/midway` (Linux) or `~/Library/Caches/midway` (macOS) |
| `CACHE_MAX_SIZE_GB` | Maximum cache size in gigabytes | `50` |
| `AWS_REGION`        | Default AWS region (used for initial bucket discovery) | `us-east-1` |

### AWS Credentials

Midway uses the standard AWS SDK credential chain. You can provide credentials via:

- Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
- AWS credentials file (`~/.aws/credentials`)
- IAM instance profile (when running on EC2)
- IAM role (when running on ECS/EKS)

## Usage

### Starting the Server

```bash
# With defaults
./midway

# With custom configuration
MIDWAY_PORT=9000 MIDWAY_MAX_SIZE_GB=100 ./midway
```

### Requesting Files

To download a file from S3 through Midway, make a GET request using the pattern:

```
GET http://localhost:8900/{bucket}/{key}
```

#### Examples

```bash
# Download a file from the "my-bucket" bucket
curl http://localhost:8900/my-bucket/path/to/file.zip -o file.zip

# Download with URL-encoded paths
curl http://localhost:8900/my-bucket/folder%20name/file.apk -o file.apk
```

On first request, Midway downloads the file from S3 and caches it locally. Subsequent requests for the same file are served directly from disk.

## API Endpoints

### `GET /{bucket}/{key...}`

Downloads a file from S3 (or serves from cache if available).

**Response**: The file contents with appropriate headers.

### `GET /health`

Health check endpoint.

**Response**:
```json
{
  "status": "ok"
}
```

### `GET /stats`

Returns cache statistics.

**Response**:
```json
{
  "hits": 1542,
  "misses": 89,
  "evictions": 12,
  "totalBytes": 5368709120,
  "maxBytes": 53687091200,
  "entryCount": 156,
  "cacheDir": "/home/user/.cache/midway"
}
```

## How It Works

### Caching Strategy

Midway uses an LRU (Least Recently Used) eviction policy:

1. When a file is requested, Midway first checks the local cache
2. On cache hit, the file is served directly and marked as recently used
3. On cache miss, the file is downloaded from S3 and stored in the cache
4. When the cache exceeds `MIDWAY_MAX_SIZE_GB`, the least recently accessed files are evicted

### Region Detection

Midway automatically detects the region of each S3 bucket on first access:

1. Queries S3 for the bucket's location using the `GetBucketLocation` API
2. Caches the region for subsequent requests to the same bucket
3. Creates region-specific S3 clients to avoid redirect errors

This means you can access buckets in any region without configuration.

### Cache Persistence

Cache metadata is stored in `{MIDWAY_DIR}/metadata.json`. On startup, Midway:

1. Loads the metadata file
2. Verifies each cached file still exists on disk
3. Rebuilds the LRU ordering based on last access times

Cached files are stored in `{MIDWAY_DIR}/files/`.

## Docker Deployment

### Docker Compose

```yaml
version: '3.8'
services:
  midway:
    build: .
    ports:
      - "8900:8900"
    volumes:
      - midway-cache:/var/cache/midway
    environment:
      - AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}
      - AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}
      - MIDWAY_MAX_SIZE_GB=100

volumes:
  midway-cache:
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: midway
spec:
  replicas: 1
  selector:
    matchLabels:
      app: midway
  template:
    metadata:
      labels:
        app: midway
    spec:
      containers:
        - name: midway
          image: midway:latest
          ports:
            - containerPort: 8900
          env:
            - name: MIDWAY_MAX_SIZE_GB
              value: "100"
          volumeMounts:
            - name: cache
              mountPath: /var/cache/midway
      volumes:
        - name: cache
          persistentVolumeClaim:
            claimName: midway-cache
```

## Performance Considerations

- **Disk Speed**: Use SSDs for the cache directory for best performance
- **Cache Size**: Size your cache based on your working set to maximize hit rate
- **Network**: Deploy Midway close to your application to minimize latency
- **Concurrency**: Midway handles concurrent requests safely with proper locking

## License

MIT License - see [LICENSE](LICENSE) for details.
