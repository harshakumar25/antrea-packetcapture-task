# Build stage
FROM golang:1.23 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /controller ./cmd/controller

# Runtime stage
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    tcpdump \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /controller /controller
RUN mkdir -p /capture
ENTRYPOINT ["/controller"]
