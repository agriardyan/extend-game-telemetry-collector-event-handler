# Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
# This is licensed software from AccelByte Inc, for limitations
# and restrictions contact your company contract manager.

# ----------------------------------------
# Stage 1: Protoc Code Generation
# ----------------------------------------
FROM golang:1.24-alpine AS proto-builder

# Install build dependencies and protoc
RUN apk add --no-cache \
    bash \
    ca-certificates \
    curl \
    git \
    protobuf \
    protobuf-dev

# Install protoc Go tools and plugins
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest \
    && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Set working directory
WORKDIR /build

# Copy proto sources and generator script
COPY proto.sh .
COPY pkg/proto/ pkg/proto/

# Generate protobuf files.
RUN chmod +x proto.sh && \
    ./proto.sh



# ----------------------------------------
# Stage 2: gRPC Server Builder
# ----------------------------------------
FROM golang:1.24-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

ARG GOOS=$TARGETOS
ARG GOARCH=$TARGETARCH
ARG CGO_ENABLED=0

# Set working directory
WORKDIR /build

# Copy and download the dependencies for application
COPY go.mod go.sum ./
RUN go mod download

# Copy config/pipeline.yaml for embedding into the binary
COPY config/pipeline.yaml config/pipeline.yaml

# Copy application code
COPY . .

# Copy generated protobuf files from stage 1
COPY --from=proto-builder /build/pkg/pb pkg/pb

# Build the Go application binary for the target OS and architecture
RUN go build -v -modcacherw -o /output/extend-churn-intervention .


# ----------------------------------------
# Stage 3: Runtime Container
# ----------------------------------------
FROM alpine:3.22

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    bash \
    curl

# Set working directory.
WORKDIR /app

# Copy build
COPY --from=builder /output/extend-churn-intervention /app/main
COPY --from=builder /build/config/pipeline.yaml /app/config/pipeline.yaml

# Plugin Arch gRPC Server Port.
EXPOSE 6565

# Prometheus /metrics Web Server Port.
EXPOSE 8080

# Entrypoint
ENTRYPOINT ["/app/main"]
