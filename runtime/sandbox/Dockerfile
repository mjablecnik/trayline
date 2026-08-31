# Sandboxed dev container with Go, Node, Bun, Flutter, Kotlin/Quarkus, Godot, Kiro CLI, Claude Code & Cline CLI
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Base tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl wget unzip git ca-certificates gnupg xz-utils openssh-client \
    clang cmake ninja-build pkg-config libgtk-3-dev liblzma-dev libstdc++-12-dev \
    ffmpeg libimage-exiftool-perl jq \
    lld libsqlite3-dev sqlite3 xdg-user-dirs tmux sudo \
    xvfb xdotool scrot \
    && rm -rf /var/lib/apt/lists/*

# Go
ARG GO_VERSION=1.25.13
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"

# Node.js (LTS)
RUN curl -fsSL https://deb.nodesource.com/setup_24.x | bash - \
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
ARG FLUTTER_VERSION=3.47.1
RUN git clone --depth 1 --branch ${FLUTTER_VERSION} https://github.com/flutter/flutter.git /opt/flutter
ENV PATH="/opt/flutter/bin:/opt/flutter/bin/cache/dart-sdk/bin:${PATH}"
RUN flutter precache && flutter config --no-analytics && dart --disable-analytics

# Kotlin + Quarkus (requires JDK)
ARG JAVA_VERSION=21
ARG KOTLIN_VERSION=2.1.10
ARG QUARKUS_VERSION=3.21.3
RUN apt-get update && apt-get install -y --no-install-recommends \
    openjdk-${JAVA_VERSION}-jdk-headless \
    && rm -rf /var/lib/apt/lists/*
ENV JAVA_HOME="/usr/lib/jvm/java-${JAVA_VERSION}-openjdk-amd64"
ENV PATH="${JAVA_HOME}/bin:${PATH}"
# Kotlin compiler
RUN curl -fsSL "https://github.com/JetBrains/kotlin/releases/download/v${KOTLIN_VERSION}/kotlin-compiler-${KOTLIN_VERSION}.zip" \
    -o /tmp/kotlin.zip \
    && unzip -q /tmp/kotlin.zip -d /opt \
    && rm /tmp/kotlin.zip
ENV PATH="/opt/kotlinc/bin:${PATH}"
# Quarkus CLI (via JBang)
RUN curl -Ls https://sh.jbang.dev | bash -s - app setup \
    && /root/.jbang/bin/jbang trust add https://repo1.maven.org/maven2/io/quarkus/quarkus-cli/ \
    && /root/.jbang/bin/jbang app install --fresh --force quarkus@quarkusio \
    && ln -sf /root/.jbang/bin/quarkus /usr/local/bin/quarkus
# Gradle (Quarkus build tool)
ARG GRADLE_VERSION=8.14.2
RUN curl -fsSL "https://services.gradle.org/distributions/gradle-${GRADLE_VERSION}-bin.zip" \
    -o /tmp/gradle.zip \
    && unzip -q /tmp/gradle.zip -d /opt \
    && rm /tmp/gradle.zip
ENV PATH="/opt/gradle-${GRADLE_VERSION}/bin:${PATH}"

# Godot Engine (headless-capable editor + export templates)
# The standard Linux binary supports `--headless`, so a single build serves both
# interactive editing (via Xvfb) and CI-style project/export commands. Export
# templates are needed for `godot --headless --export-release`.
# Runtime graphics libraries Godot needs even for offscreen/headless rendering.
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgl1 libglu1-mesa libglx-mesa0 libx11-6 libxcursor1 libxinerama1 \
    libxi6 libxrandr2 libxext6 libudev1 libfontconfig1 \
    && rm -rf /var/lib/apt/lists/*
ARG GODOT_VERSION=4.7.1
RUN curl -fsSL "https://github.com/godotengine/godot/releases/download/${GODOT_VERSION}-stable/Godot_v${GODOT_VERSION}-stable_linux.x86_64.zip" \
      -o /tmp/godot.zip \
    && unzip -q /tmp/godot.zip -d /tmp/godot \
    && install -m 755 "/tmp/godot/Godot_v${GODOT_VERSION}-stable_linux.x86_64" /usr/local/bin/godot \
    && rm -rf /tmp/godot /tmp/godot.zip
# Export templates (installed into the shared location the agent user reads at runtime)
RUN curl -fsSL "https://github.com/godotengine/godot/releases/download/${GODOT_VERSION}-stable/Godot_v${GODOT_VERSION}-stable_export_templates.tpz" \
      -o /tmp/templates.tpz \
    && unzip -q /tmp/templates.tpz -d /tmp/templates \
    && mkdir -p "/opt/godot/export_templates/${GODOT_VERSION}.stable" \
    && cp -r /tmp/templates/templates/. "/opt/godot/export_templates/${GODOT_VERSION}.stable/" \
    && chmod -R a+rX /opt/godot \
    && rm -rf /tmp/templates /tmp/templates.tpz

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

# Playwright npm package (browser binary already installed above)
RUN npm install -g playwright

# Docker CLI (for controlling host Docker)
# Clean stale lists/keys from prior layers to avoid GPG signature errors
RUN rm -rf /var/lib/apt/lists/* /etc/apt/keyrings/docker.asc \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc \
    && echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu noble stable" \
    > /etc/apt/sources.list.d/docker.list \
    && apt-get update && apt-get install -y --no-install-recommends docker-ce-cli docker-compose-plugin \
    && rm -rf /var/lib/apt/lists/*

# GitHub CLI (gh)
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
      -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
      > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# Fly CLI (flyctl) — deploy and manage apps on fly.io.
# Installed system-wide to /usr/local/share/fly (not $HOME/.fly) so both root and the
# agent user can run it, and so it never collides with the agent's ~/.fly, which holds
# only the login token/config mounted in from the host (see runtime/trayline-agent and
# remote/docker/container.go) — the agent never needs to `fly auth login` in-container.
ENV FLYCTL_INSTALL=/usr/local/share/fly
RUN curl -L https://fly.io/install.sh | sh -s -- --non-interactive \
    && ln -sf /usr/local/share/fly/bin/flyctl /usr/local/bin/flyctl \
    && ln -sf /usr/local/share/fly/bin/fly /usr/local/bin/fly \
    && chmod -R a+rX /usr/local/share/fly

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
# Allow agent to install packages via sudo (apt-get only)
RUN echo "agent ALL=(ALL) NOPASSWD: /usr/bin/apt-get, /usr/bin/dpkg" > /etc/sudoers.d/agent \
    && chmod 0440 /etc/sudoers.d/agent
RUN mkdir -p /home/agent/.kiro /home/agent/.local/share/kiro-cli /home/agent/.claude /home/agent/.cline /home/agent/.fly /home/agent/.ssh /home/agent/.config/gh /home/agent/go
# Point Godot at the pre-installed export templates (it looks in ~/.local/share/godot)
RUN mkdir -p /home/agent/.local/share/godot \
    && ln -sf /opt/godot/export_templates /home/agent/.local/share/godot/export_templates
# Flutter SDK stays root-owned (read-only for agent); only pub-cache needs write access
RUN chmod -R a+rX /opt/flutter \
    && chmod -R a+w /opt/flutter/bin/cache \
    && chmod -R a+w /opt/flutter/packages/flutter_tools/.dart_tool \
    && mkdir -p /opt/flutter/dev /opt/flutter/examples \
    && chmod a+w /opt/flutter/.git/refs/heads \
    && mkdir -p /home/agent/.pub-cache \
    && cp -r /root/.cargo /home/agent/.cargo \
    && cp -r /root/.rustup /home/agent/.rustup \
    && cp -r /root/.jbang /home/agent/.jbang \
    && chown -R agent:agent /home/agent

# Ensure lld and llvm-ar are available where Dart/Flutter native assets expect them
RUN LLVM_DIR=$(ls -d /usr/lib/llvm-* 2>/dev/null | head -1) \
    && if [ -n "$LLVM_DIR" ]; then \
         ln -sf /usr/bin/ld.lld "$LLVM_DIR/bin/ld.lld" 2>/dev/null || true; \
         ln -sf /usr/bin/llvm-ar "$LLVM_DIR/bin/llvm-ar" 2>/dev/null || true; \
       fi

ENV PATH="/home/agent/.jbang/bin:/home/agent/.cargo/bin:/home/agent/go/bin:/usr/local/go/bin:/opt/kotlinc/bin:/opt/gradle-8.14.2/bin:/opt/flutter/bin:/opt/flutter/bin/cache/dart-sdk/bin:${PATH}"
ENV GOPATH="/home/agent/go"
ENV CARGO_HOME="/home/agent/.cargo"
ENV RUSTUP_HOME="/home/agent/.rustup"
ENV JBANG_DIR="/home/agent/.jbang"
ENV PUB_CACHE="/home/agent/.pub-cache"
ENV HOME="/home/agent"

# Workspace – mount point for project files
RUN mkdir -p /workspace /tmp/uploads
RUN chown -R agent:agent /workspace /tmp/uploads
WORKDIR /workspace

USER agent
