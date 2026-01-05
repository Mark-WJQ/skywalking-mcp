# Licensed to the Apache Software Foundation (ASF) under one or more
# contributor license agreements.  See the NOTICE file distributed with
# this work for additional information regarding copyright ownership.
# The ASF licenses this file to You under the Apache License, Version 2.0
# (the "License"); you may not use this file except in compliance with
# the License.  You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Build stage
FROM golang:1.24-bullseye AS builder

# Default version
ARG VERSION="dev"

# Set the working directory
WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./

# Go get dependencies
RUN go mod tidy

# Copy the source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=$(git rev-parse HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o bin/swmcp ./cmd/skywalking-mcp/main.go

# Make a stage to run the app
FROM debian:bullseye-slim
# 1. 首先临时使用HTTP协议（避免证书问题）
RUN sed -i 's|https://|http://|g' /etc/apt/sources.list

# 2. 确保使用国内镜像源（示例使用阿里云）
RUN sed -i 's|http://deb.debian.org|http://mirrors.aliyun.com|g' /etc/apt/sources.list && \
    sed -i 's|http://security.debian.org|http://mirrors.aliyun.com/debian-security|g' /etc/apt/sources.list

# 3. 更新并安装必要组件（分步执行）
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        apt-transport-https \
        gnupg2 \
        ca-certificates

# 4. 恢复HTTPS源（现在已有证书支持HTTPS）
RUN sed -i 's|http://|https://|g' /etc/apt/sources.list && \
    apt-get update

# 5. 清理缓存
RUN rm -rf /var/lib/apt/lists/*

# Create a non-root user
RUN useradd -r -u 1000 -m skywalking-mcp

# Set the working directory
WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder --chown=1000:1000 /app/bin/swmcp /app/

# Use the non-root user
USER skywalking-mcp

# Expose the port the app runs on
EXPOSE 8000

# Run the application, defaulting to SSE transport
ENTRYPOINT ["/app/swmcp", "stdio"]