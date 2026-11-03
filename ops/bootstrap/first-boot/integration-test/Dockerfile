# Block 68A integration test for first-boot.sh.
#
# Validates the full first-boot flow without requiring real Orin/Karbon
# hardware: USB-userdata absorption, schema validation, secrets prompt
# (driven by `expect`), envrc rendering, group_vars overlay, mock
# site-bootstrap.yml handoff, JSONL evidence emission, state marker.
#
# Build:
#   docker build -t goat-firstboot-test ops/bootstrap/first-boot/integration-test/
#
# Run:
#   ./run-test.sh
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      ansible \
      ca-certificates \
      curl \
      expect \
      jq \
      python3 \
      python3-pip \
      sudo \
      util-linux \
    && rm -rf /var/lib/apt/lists/*

# yq (Go binary) for YAML→JSON in first-boot.sh — arch-matched.
RUN ARCH=$(dpkg --print-architecture) \
    && curl -fsSL "https://github.com/mikefarah/yq/releases/download/v4.40.5/yq_linux_${ARCH}" \
      -o /usr/local/bin/yq \
    && chmod +x /usr/local/bin/yq

# check-jsonschema for first-boot.sh schema validation.
# Ubuntu 24.04 has PEP 668 marker; --break-system-packages is intentional
# for this isolated test container.
RUN pip install --break-system-packages check-jsonschema

# Repo will be bind-mounted at /opt/goat/repo by the test driver
# at run time; pre-create the path so first-boot.sh's REPO_ROOT default
# matches without needing an env override.
RUN mkdir -p /opt/goat /var/lib/goat /var/log/goat \
             /etc/goat

# Test driver entrypoint.
COPY drive-test.sh /usr/local/bin/drive-test.sh
RUN chmod +x /usr/local/bin/drive-test.sh

CMD ["/usr/local/bin/drive-test.sh"]
