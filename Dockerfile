# Sandboxed dev container with Go, Node, Bun, Flutter, Kiro CLI & Claude Code
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Base tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl wget unzip git ca-certificates gnupg xz-utils \
    clang cmake ninja-build pkg-config libgtk-3-dev liblzma-dev libstdc++-12-dev \
    && rm -rf /var/lib/apt/lists/*

# Go
ARG GO_VERSION=1.23.6
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"

# Node.js (LTS)
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs && rm -rf /var/lib/apt/lists/*

# Bun
RUN curl -fsSL https://bun.sh/install | bash
ENV PATH="/root/.bun/bin:${PATH}"

# Python + uv
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 python3-pip python3-venv \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/bin/python3 /usr/bin/python
RUN curl -LsSf https://astral.sh/uv/install.sh | sh \
    && cp /root/.local/bin/uv /usr/local/bin/ \
    && cp /root/.local/bin/uvx /usr/local/bin/

# Flutter
ARG FLUTTER_VERSION=3.27.4
RUN git clone --depth 1 --branch ${FLUTTER_VERSION} https://github.com/flutter/flutter.git /opt/flutter
ENV PATH="/opt/flutter/bin:/opt/flutter/bin/cache/dart-sdk/bin:${PATH}"
RUN flutter precache && flutter config --no-analytics && dart --disable-analytics

# Kiro CLI (manual install – install.sh refuses root)
RUN curl --proto '=https' --tlsv1.2 -sSf \
    'https://desktop-release.q.us-east-1.amazonaws.com/latest/kirocli-x86_64-linux.zip' \
    -o /tmp/kirocli.zip \
    && unzip /tmp/kirocli.zip -d /tmp/kirocli \
    && install -m 755 /tmp/kirocli/kirocli/bin/* /usr/local/bin/ \
    && rm -rf /tmp/kirocli /tmp/kirocli.zip

# Claude Code CLI
RUN npm install -g @anthropic-ai/claude-code

# Docker CLI (for controlling host Docker)
# Clean stale lists/keys from prior layers to avoid GPG signature errors
RUN rm -rf /var/lib/apt/lists/* /etc/apt/keyrings/docker.asc \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc \
    && echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable" \
    > /etc/apt/sources.list.d/docker.list \
    && apt-get update && apt-get install -y --no-install-recommends docker-ce-cli docker-compose-plugin \
    && rm -rf /var/lib/apt/lists/*

# Non-root user (Claude Code refuses --dangerously-skip-permissions as root)
RUN userdel -r ubuntu 2>/dev/null; useradd -m -s /bin/bash -u 1000 agent 
RUN mkdir -p /home/agent/.kiro /home/agent/.local/share/kiro-cli /home/agent/.claude /home/agent/go   
RUN chown -R agent:agent /home/agent /opt/flutter 

ENV PATH="/home/agent/.bun/bin:/home/agent/go/bin:/usr/local/go/bin:/opt/flutter/bin:/opt/flutter/bin/cache/dart-sdk/bin:${PATH}"
ENV GOPATH="/home/agent/go"
ENV HOME="/home/agent"

# Workspace – mount point for project files
RUN mkdir -p /workspace
RUN chown -R agent:agent /workspace 
WORKDIR /workspace

USER agent
