# Sandboxed dev container with Go, Node, Bun, Flutter, Kiro CLI, Claude Code & Cline CLI
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Base tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl wget unzip git ca-certificates gnupg xz-utils \
    clang cmake ninja-build pkg-config libgtk-3-dev liblzma-dev libstdc++-12-dev \
    ffmpeg libimage-exiftool-perl jq \
    && rm -rf /var/lib/apt/lists/*

# Go
ARG GO_VERSION=1.23.6
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"

# Node.js (LTS)
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs && rm -rf /var/lib/apt/lists/*

# Bun (installed globally so both root and agent can use it)
RUN curl -fsSL https://bun.sh/install | BUN_INSTALL=/usr/local bash
ENV PATH="/usr/local/bin:${PATH}"

# Python + uv
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 python3-pip python3-venv \
    && rm -rf /var/lib/apt/lists/* \
    && ln -sf /usr/bin/python3 /usr/bin/python
RUN curl -LsSf https://astral.sh/uv/install.sh | sh \
    && cp /root/.local/bin/uv /usr/local/bin/ \
    && cp /root/.local/bin/uvx /usr/local/bin/

# Rust
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

# Flutter
ARG FLUTTER_VERSION=3.38.5
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

# Cline CLI
RUN npm install -g cline

# Docker CLI (for controlling host Docker)
# Clean stale lists/keys from prior layers to avoid GPG signature errors
RUN rm -rf /var/lib/apt/lists/* /etc/apt/keyrings/docker.asc \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc \
    && echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable" \
    > /etc/apt/sources.list.d/docker.list \
    && apt-get update && apt-get install -y --no-install-recommends docker-ce-cli docker-compose-plugin \
    && rm -rf /var/lib/apt/lists/*

# Playwright system dependencies (Chromium headless shell)
RUN apt-get update && apt-get install -y --no-install-recommends \
    libnspr4 libnss3 libnss3-tools libatk1.0-0t64 libatk-bridge2.0-0t64 \
    libcups2t64 libdrm2 libxkbcommon0 libxcomposite1 libxdamage1 \
    libxfixes3 libxrandr2 libgbm1 libpango-1.0-0 libcairo2 libasound2t64 \
    libatspi2.0-0t64 libwayland-client0 \
    && rm -rf /var/lib/apt/lists/*

# Install Playwright Chromium browser binary (shared, readable by all users)
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/playwright-browsers
RUN npx playwright install chromium \
    && chmod -R a+rX /opt/playwright-browsers

# Create a convenient 'chromium' wrapper so agent can run it easily
RUN CHROMIUM_BIN=$(find /opt/playwright-browsers -name "chrome" -path "*/chrome-linux/*" | head -1) \
    && printf '#!/bin/bash\nexec %s --no-sandbox "$@"\n' "$CHROMIUM_BIN" > /usr/local/bin/chromium \
    && chmod +x /usr/local/bin/chromium

# Verify Playwright system deps are satisfied (will error if libs are missing)
RUN npx playwright install-deps chromium 2>/dev/null || true

# Non-root user (Claude Code refuses --dangerously-skip-permissions as root)
RUN userdel -r ubuntu 2>/dev/null; useradd -m -s /bin/bash -u 1000 agent 
RUN mkdir -p /home/agent/.kiro /home/agent/.local/share/kiro-cli /home/agent/.claude /home/agent/.cline /home/agent/go   
# Flutter SDK stays root-owned (read-only for agent); only pub-cache needs write access
RUN chmod -R a+rX /opt/flutter \
    && chmod -R a+w /opt/flutter/bin/cache \
    && chmod -R a+w /opt/flutter/packages/flutter_tools/.dart_tool \
    && mkdir -p /opt/flutter/dev /opt/flutter/examples \
    && chmod a+w /opt/flutter/.git/refs/heads \
    && mkdir -p /home/agent/.pub-cache \
    && cp -r /root/.cargo /home/agent/.cargo \
    && cp -r /root/.rustup /home/agent/.rustup \
    && chown -R agent:agent /home/agent

# Ensure ld.lld is available where Flutter expects it (next to clang++ binary)
RUN ln -sf /usr/bin/ld /usr/lib/llvm-*/bin/ld.lld 2>/dev/null || true

ENV PATH="/home/agent/.cargo/bin:/home/agent/go/bin:/usr/local/go/bin:/opt/flutter/bin:/opt/flutter/bin/cache/dart-sdk/bin:${PATH}"
ENV GOPATH="/home/agent/go"
ENV CARGO_HOME="/home/agent/.cargo"
ENV RUSTUP_HOME="/home/agent/.rustup"
ENV PUB_CACHE="/home/agent/.pub-cache"
ENV HOME="/home/agent"

# Workspace – mount point for project files
RUN mkdir -p /workspace /tmp/uploads
RUN chown -R agent:agent /workspace /tmp/uploads
WORKDIR /workspace

USER agent
