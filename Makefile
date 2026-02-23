# Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
# This is licensed software from AccelByte Inc, for limitations
# and restrictions contact your company contract manager.

SHELL := /bin/bash

IMAGE_NAME := extend-game-telemetry-collector-event-handler
IMAGE_TAG ?= latest
PROTOC_IMAGE := proto-builder

.PHONY: build proto_image proto test clean

# Build the Docker image (includes proto generation)
build:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

# Build only the proto-builder stage (for development/debugging)
proto_image:
	docker build --target proto-builder -t $(PROTOC_IMAGE) .

# Generate protobuf files locally (for development)
proto: proto_image
	docker run --tty --rm --user $$(id -u):$$(id -g) \
		--volume $$(pwd):/build \
		--workdir /build \
		--entrypoint /bin/bash \
		$(PROTOC_IMAGE) \
			proto.sh

# Run unit tests
test:
	go test -v -cover ./...

# Clean generated files and Docker images
clean:
	rm -rf pkg/pb/*
	docker rmi -f $(IMAGE_NAME):$(IMAGE_TAG) $(PROTOC_IMAGE) 2>/dev/null || true
