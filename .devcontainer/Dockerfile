FROM mcr.microsoft.com/devcontainers/go:1.26-bookworm AS builder

RUN apt-get update && apt-get install -y curl tar unzip && rm -rf /var/lib/apt/lists/*

ARG TARGETARCH
ARG TASK_VERSION=v3.45.4
ARG CURL_OPTS="--retry 3 --retry-delay 5 --retry-connrefused"

# Download go-task
RUN curl ${CURL_OPTS} -L -o /tmp/task.tar.gz "https://github.com/go-task/task/releases/download/${TASK_VERSION}/task_linux_${TARGETARCH}.tar.gz" && \
    tar -xzf /tmp/task.tar.gz -C /tmp && \
    chmod +x /tmp/task

# Download Zig (C/C++ cross-compiler for CGO + llama.cpp embedding)
# Note: Zig does not publish prebuilt binaries on GitHub Releases (source-only),
# so we must download from ziglang.org.
ARG ZIG_VERSION=0.15.2
RUN mkdir -p /tmp/zig && \
    ZIG_ARCH=$(if [ "${TARGETARCH}" = "amd64" ]; then echo "x86_64"; else echo "aarch64"; fi) && \
    curl ${CURL_OPTS} -fL -o /tmp/zig.tar.xz "https://ziglang.org/download/${ZIG_VERSION}/zig-${ZIG_ARCH}-linux-${ZIG_VERSION}.tar.xz" && \
    tar xJf /tmp/zig.tar.xz --strip-components=1 -C /tmp/zig && \
    rm /tmp/zig.tar.xz

# Download Bun (for TypeScript tooling / MCP context7)
ARG BUN_VERSION=bun-v1.3.11
RUN BUN_ARCH=$(if [ "${TARGETARCH}" = "amd64" ]; then echo "x64"; else echo "aarch64"; fi) && \
    curl ${CURL_OPTS} -L -o /tmp/bun.zip "https://github.com/oven-sh/bun/releases/download/${BUN_VERSION}/bun-linux-${BUN_ARCH}.zip" && \
    cd /tmp && unzip -q bun.zip && \
    chmod +x /tmp/bun-linux-${BUN_ARCH}/bun

# Download golangci-lint
ARG GOLANGCI_LINT_VERSION=v2.1.6
RUN curl ${CURL_OPTS} -fL -o /tmp/golangci-lint.tar.gz \
    "https://github.com/golangci/golangci-lint/releases/download/${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION#v}-linux-${TARGETARCH}.tar.gz" && \
    tar -xzf /tmp/golangci-lint.tar.gz -C /tmp && \
    chmod +x /tmp/golangci-lint-${GOLANGCI_LINT_VERSION#v}-linux-${TARGETARCH}/golangci-lint && \
    mv /tmp/golangci-lint-${GOLANGCI_LINT_VERSION#v}-linux-${TARGETARCH}/golangci-lint /tmp/golangci-lint && \
    rm /tmp/golangci-lint.tar.gz

# Download GitHub Copilot CLI
ARG COPILOT_VERSION=v1.0.18
RUN COPILOT_ARCH=$(if [ "${TARGETARCH}" = "amd64" ]; then echo "x64"; else echo "arm64"; fi) && \
    curl ${CURL_OPTS} -L -o /tmp/copilot.tar.gz "https://github.com/github/copilot-cli/releases/download/${COPILOT_VERSION}/copilot-linux-${COPILOT_ARCH}.tar.gz" && \
    mkdir -p /tmp/copilot && tar -xzf /tmp/copilot.tar.gz -C /tmp/copilot && \
    rm /tmp/copilot.tar.gz

FROM mcr.microsoft.com/devcontainers/go:1.26-bookworm AS development

COPY --from=builder /tmp/task /usr/local/bin/
COPY --from=builder /tmp/golangci-lint /usr/local/bin/

# Copy Zig compiler
COPY --from=builder /tmp/zig /usr/local/zig
ENV PATH="/usr/local/zig:${PATH}"

# Copy bun (handle both architectures)
ARG TARGETARCH
RUN --mount=from=builder,source=/tmp,target=/mnt/builder \
    BUN_ARCH=$(if [ "${TARGETARCH}" = "amd64" ]; then echo "x64"; else echo "aarch64"; fi) && \
    cp /mnt/builder/bun-linux-${BUN_ARCH}/bun /usr/local/bin/bun && \
    ln -s /usr/local/bin/bun /usr/local/bin/bunx

# Copy GitHub Copilot CLI
COPY --from=builder /tmp/copilot /usr/local/lib/copilot
RUN ln -s /usr/local/lib/copilot/copilot /usr/local/bin/copilot