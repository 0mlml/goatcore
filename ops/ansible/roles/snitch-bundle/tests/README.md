# snitch-bundle role tests

Parse-only smoke. End-to-end probe-bundle delivery + per-platform
installer dispatch is covered by the per-OS installer scripts at
[`ops/snitch/install-probe.sh`](../../../../snitch/install-probe.sh) +
[`install-probe-darwin.sh`](../../../../snitch/install-probe-darwin.sh) +
[`install-probe-windows.ps1`](../../../../snitch/install-probe-windows.ps1)
and the Block 61H multi-platform CI build matrix at
[`.github/workflows/snitch.yml`](../../../../../.github/workflows/snitch.yml).

Run: `./syntax-check.sh`.
