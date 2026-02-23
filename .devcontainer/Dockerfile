# Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
# This is licensed software from AccelByte Inc, for limitations
# and restrictions contact your company contract manager.

FROM --platform=$BUILDPLATFORM ubuntu:22.04

# Avoid warnings by switching to noninteractive
ENV DEBIAN_FRONTEND=noninteractive

# Set up Go environment
ARG TARGETOS
ARG TARGETARCH

ARG GOOS=$TARGETOS
ARG GOARCH=$TARGETARCH

ENV GOROOT=/usr/local/go
ENV GOPATH=/go
ENV PATH=$GOPATH/bin:$GOROOT/bin:$PATH

ARG PROTOC_VERSION=21.9
ARG GO_VERSION=1.24.10
ARG HOST_OS

# Configure apt and install packages
RUN apt-get update \
    && apt-get -y install --no-install-recommends \
    #
    # Install essential development tools
    build-essential \
    ca-certificates \
    git \
    python3-pip \
    sudo \
    unzip \
    wget \
    #
    # Detect architecture for downloads
    && ARCH_SUFFIX=$(case "$(uname -m)" in \
        x86_64) echo "x86_64" ;; \
        aarch64) echo "aarch_64" ;; \
        *) echo "x86_64" ;; \
       esac) \
    && GOARCH_SUFFIX=$(case "$(uname -m)" in \
        x86_64) echo "amd64" ;; \
        aarch64) echo "arm64" ;; \
        *) echo "amd64" ;; \
       esac) \
    # Install Protocol Buffers compiler
    && wget -O protoc.zip https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-${ARCH_SUFFIX}.zip \
    && unzip protoc.zip -d /usr/local \
    && rm protoc.zip \
    && chmod +x /usr/local/bin/protoc \
    #
    # Install Go
    && wget -O go.tar.gz https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH_SUFFIX}.tar.gz \
    && tar -C /usr/local -xzf go.tar.gz \
    && rm go.tar.gz \
    #
    # Install additional tools
    && pip3 install --no-cache-dir fillenv \
    #
    # Clean up
    && apt-get autoremove -y \
    && apt-get clean -y \
    && rm -rf /var/lib/apt/lists/*

# Create a non-root user
ARG USERNAME=vscode
ARG USER_UID=1000
ARG USER_GID=$USER_UID

# Create user group, user account, and configure passwordless sudo access
RUN groupadd --gid $USER_GID $USERNAME \
    && useradd --uid $USER_UID --gid $USER_GID -m $USERNAME \
    && echo $USERNAME ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/$USERNAME \
    && chmod 0440 /etc/sudoers.d/$USERNAME

# Create workspace and GOPATH directories
RUN mkdir -p /workspace && chown $USERNAME:$USERNAME /workspace \
    && mkdir -p /go && chown $USERNAME:$USERNAME /go

# Switch to non-root user
USER $USERNAME

# Install Go Language Server (required by Go extension)
RUN go install -v golang.org/x/tools/gopls@latest

# Install Go Debugger
RUN go install -v github.com/go-delve/delve/cmd/dlv@latest

# Install protoc Go tools and plugins
RUN go install -v google.golang.org/protobuf/cmd/protoc-gen-go@latest \
    && go install -v google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest \
    && go install -v github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest \
    && go install -v github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest

# Switch back to dialog for any ad-hoc use of apt-get
ENV DEBIAN_FRONTEND=dialog

WORKDIR /workspace
