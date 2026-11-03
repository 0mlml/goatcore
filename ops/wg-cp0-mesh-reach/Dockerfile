# wg-cp0 mesh-reach bench-env container image (ADR 1017 substrate-invariant test).
#
# Single image, role selected at runtime via $ROLE={peer,relay} env var read by
# the entrypoint. Kernel WireGuard via wg-quick; container needs NET_ADMIN cap
# and the host must have the wireguard kernel module loaded (default on Linux
# 5.6+; standard on GitHub Actions ubuntu-24.04 runners).
#
# Image size kept small (~12 MiB) since N+M instances boot per harness run.

FROM alpine:3.20

RUN apk add --no-cache \
    wireguard-tools \
    iproute2 \
    iputils \
    bash \
    && mkdir -p /etc/wireguard /work

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
