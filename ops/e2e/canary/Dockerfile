# e2e-canary — the smallest image the deployment engineering can move.
# busybox so the container can sleep; one file so each build has its own digest.
# Built by .github/workflows/e2e-canary-image.yml (ghcr.io/usareur-af/e2e-canary)
# and, in the hermetic tier, by ops/e2e/hermetic.sh into a local registry.
FROM busybox:1.37.0-musl@sha256:5cec3fc171c87218698e85a52af7087de727372aae264a787b8112901a5b0092
ARG CANARY_STAMP=unstamped
RUN printf 'e2e-canary %s\n' "$CANARY_STAMP" > /canary.txt
CMD ["sh", "-c", "cat /canary.txt; exec sleep 2147483647"]
