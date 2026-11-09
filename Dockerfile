# Stage 1: Build the Go binary
FROM golang:1.27 AS builder

WORKDIR /app

# Copy Go module files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy application source code
COPY main.go ./
COPY database/ ./database/
COPY handlers/ ./handlers/
COPY middleware/ ./middleware/

# Build static Linux binary for Google Cloud Run
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server .

# Stage 2: Minimal production image
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy compiled binary, HTML templates, and static assets (CSS, JS, resume.pdf)
COPY --from=builder /app/server ./server
COPY templates/ ./templates/
COPY static/ ./static/

ENV PORT=8080
EXPOSE 8080

CMD ["./server"]

