#!/usr/bin/env python3
"""
Structural test for the `netbird-dashboard` Ansible role.

Asserts that the nine load-bearing fields surfaced during 2026-05-07 →
2026-05-08 first-dogfood (Block 21D.1) remain present + correctly
shaped in the role's defaults + tasks. Guards against future commits
that remove one of the fields and silently break OIDC/JWT/auth.

Each finding-backed invariant is a separate test method that prints a
clear pass/fail line so a CI failure log makes the regression obvious.

Usage (no docker, no Ansible runner needed):
    python3 ops/ansible/roles/netbird-dashboard/tests/test-structure.py

Exit code 0 on all-pass, 1 on any-fail.

CI: .github/workflows/netbird-dashboard-role.yml on changes to
    ops/ansible/roles/netbird-dashboard/**.
"""

import sys
from pathlib import Path

import yaml

ROLE_ROOT = Path(__file__).resolve().parent.parent
DEFAULTS = ROLE_ROOT / "defaults" / "main.yml"
TASKS = ROLE_ROOT / "tasks" / "main.yml"
HANDLERS = ROLE_ROOT / "handlers" / "main.yml"
ENV_TEMPLATE = ROLE_ROOT / "templates" / "dashboard.env.j2"


class T:
    """Lightweight test runner with finding cross-refs in failure messages."""

    def __init__(self):
        self.failed = []
        self.passed = 0

    def assert_eq(self, label: str, got, want, finding: str = ""):
        if got == want:
            self.passed += 1
            print(f"  PASS  {label}")
        else:
            msg = f"  FAIL  {label}: got={got!r} want={want!r}"
            if finding:
                msg += f"  ({finding})"
            print(msg)
            self.failed.append(label)

    def assert_in(self, label: str, needle, haystack, finding: str = ""):
        if needle in haystack:
            self.passed += 1
            print(f"  PASS  {label}")
        else:
            msg = f"  FAIL  {label}: {needle!r} not in {haystack!r}"
            if finding:
                msg += f"  ({finding})"
            print(msg)
            self.failed.append(label)

    def assert_contains(self, label: str, text: str, substr: str, finding: str = ""):
        if substr in text:
            self.passed += 1
            print(f"  PASS  {label}")
        else:
            msg = f"  FAIL  {label}: {substr!r} not in file"
            if finding:
                msg += f"  ({finding})"
            print(msg)
            self.failed.append(label)

    def report(self) -> int:
        print()
        print(f"  {self.passed} passed, {len(self.failed)} failed")
        return 0 if not self.failed else 1


def main() -> int:
    print("netbird-dashboard role structural test")
    print("=" * 60)

    defaults = yaml.safe_load(DEFAULTS.read_text())
    tasks_text = TASKS.read_text()
    handlers = yaml.safe_load(HANDLERS.read_text())
    env_template = ENV_TEMPLATE.read_text()

    t = T()

    # -- Item 1: skip_consent default + tasks reference --
    print("\n[1/9] F-077 skip_consent: true (Hydra consent UI silently grants 0 scopes)")
    t.assert_eq(
        "defaults: netbird_dashboard_oidc_skip_consent",
        defaults.get("netbird_dashboard_oidc_skip_consent"),
        True,
        finding="F-077",
    )
    t.assert_contains(
        "tasks: POST/PUT body references skip_consent",
        tasks_text,
        'skip_consent: "{{ netbird_dashboard_oidc_skip_consent }}"',
        finding="F-077",
    )

    # -- Item 2: access_token_strategy default + tasks reference + handler --
    print("\n[2/9] F-078 access_token_strategy: jwt (Hydra opaque vs mgmt JWKS-only)")
    t.assert_eq(
        "defaults: netbird_dashboard_oidc_access_token_strategy",
        defaults.get("netbird_dashboard_oidc_access_token_strategy"),
        "jwt",
        finding="F-078",
    )
    t.assert_contains(
        "tasks: POST/PUT body references access_token_strategy",
        tasks_text,
        'access_token_strategy: "{{ netbird_dashboard_oidc_access_token_strategy }}"',
        finding="F-078",
    )
    handler_names = [h.get("name") for h in handlers]
    t.assert_in(
        "handlers: restart management-a present (JWKS-cache refresh)",
        "restart management-a",
        handler_names,
        finding="F-078",
    )
    t.assert_contains(
        "tasks: POST notifies restart management-a",
        tasks_text,
        "notify: restart management-a",
        finding="F-078",
    )

    # -- Item 3 (token_endpoint_auth_method): client_secret_post --
    print("\n[3/9] token_endpoint_auth_method: client_secret_post (SPA sends form body)")
    t.assert_eq(
        "defaults: netbird_dashboard_oidc_token_endpoint_auth_method",
        defaults.get("netbird_dashboard_oidc_token_endpoint_auth_method"),
        "client_secret_post",
    )

    # -- Item 4: redirect_uris set ≠ /auth (which 404s) --
    print("\n[4/9] F-080 redirect_uris (covers OIDC-library landing routes)")
    redirect_uris = defaults.get("netbird_dashboard_oidc_redirect_uris", [])
    t.assert_eq(
        "redirect_uris contains /peers (AUTH_REDIRECT_URI default)",
        any("/peers" in u for u in redirect_uris),
        True,
        finding="F-080",
    )
    t.assert_eq(
        "redirect_uris contains /access-control (callbackPath after Traefik 302)",
        any("/access-control" in u for u in redirect_uris),
        True,
        finding="F-080",
    )
    t.assert_eq(
        "redirect_uris contains /silent-auth (AUTH_SILENT_REDIRECT_URI)",
        any("/silent-auth" in u for u in redirect_uris),
        True,
    )
    t.assert_eq(
        "redirect_uris does NOT contain /auth (404 in static export, breaks OAuth)",
        any(u.endswith("/auth") for u in redirect_uris),
        False,
        finding="F-080",
    )

    # -- Item 5 (env template): AUTH_REDIRECT_URI=/peers + AUTH_AUTHORITY trailing slash --
    print("\n[5/9] dashboard.env.j2 (AUTH_REDIRECT_URI + trailing slash on AUTH_AUTHORITY)")
    t.assert_contains(
        "env template: AUTH_REDIRECT_URI=/peers",
        env_template,
        "AUTH_REDIRECT_URI=/peers",
        finding="F-080",
    )
    # AUTH_AUTHORITY must end in trailing slash to match Hydra iss claim format.
    t.assert_contains(
        "env template: AUTH_AUTHORITY trailing slash",
        env_template,
        "AUTH_AUTHORITY=https://oauth2.{{ site_id }}.{{ site_dns_zone }}/",
    )

    # -- Item 6 (idempotency): drift-detection fact present --
    print("\n[6/9] idempotency (drift-detect set_fact gates PUT-and-notify)")
    t.assert_contains(
        "tasks: dashboard_oidc_client_needs_update set_fact present",
        tasks_text,
        "dashboard_oidc_client_needs_update",
    )
    t.assert_contains(
        "tasks: PUT gated on needs_update",
        tasks_text,
        "dashboard_oidc_client_needs_update | default(false)",
    )

    # -- Item 7 (handler shape): recreate dashboard handler exists for env_file changes --
    print("\n[7/9] handlers (recreate dashboard for env_file changes)")
    t.assert_in(
        "handlers: recreate dashboard present (env_file changes need force-recreate)",
        "recreate dashboard",
        handler_names,
    )

    # -- Item 8 (audience whitelist): client_id in audience array --
    print("\n[8/9] audience whitelist (Hydra rejects unknown audiences)")
    t.assert_contains(
        "tasks: POST body audience contains client_id",
        tasks_text,
        '- "{{ netbird_dashboard_oidc_client_id }}"',
    )

    # -- Item 9: every dashboard router has the
    # `dashboard-root-to-access-control` middleware attached, AND the
    # traefik role defaults define that middleware.
    #
    # The middleware does the bare-`/` → `/access-control` redirect at
    # the Traefik tier so it survives every dashboard container churn.
    # The prior in-container nginx patch did NOT survive churn (operator
    # re-applied it twice in two days). If a future commit removes the
    # middleware attachment from a per-site group_vars, this assertion
    # fails CI before the change ships.
    print("\n[9/9] F-080 durable: dashboard router has redirect middleware attached")
    # ROLE_ROOT = .../ops/ansible/roles/netbird-dashboard
    # .parent × 4 = repo root (.../netbird-dashboard → roles → ansible → ops → repo).
    repo_root = ROLE_ROOT.parent.parent.parent.parent
    traefik_defaults_path = (
        repo_root / "ops/ansible/roles/traefik/defaults/main.yml"
    )
    traefik_defaults = yaml.safe_load(traefik_defaults_path.read_text())
    mw_def = (
        traefik_defaults.get("traefik_middlewares", {})
        .get("dashboard-root-to-access-control", {})
    )
    t.assert_in(
        "traefik defaults: dashboard-root-to-access-control is redirectRegex",
        "redirectRegex",
        mw_def,
        finding="F-080",
    )
    t.assert_eq(
        "traefik defaults: middleware is non-permanent (302 not 301)",
        mw_def.get("redirectRegex", {}).get("permanent"),
        False,
        finding="F-080",
    )
    # Discovered, not hard-coded: this test named vega_hosts / prov_jdn_hosts /
    # prov_fra_hosts, all three retired, and it
    # then crashed on every PR trying to read a group_vars dir that no longer
    # exists — a test pinned to a name outlives the thing it names. Scan the
    # groups that actually declare a dashboard service, so a retired goatnet
    # drops out and a new one is covered without editing this file. The
    # invariant under test is per-goatnet ("wherever a dashboard is served, its
    # router carries the access-control middleware"), so having zero today is a
    # legitimate state, not a test gap.
    gv_root = repo_root / "ops/ansible/group_vars"
    sites_with_dashboard = []
    for gv_path in sorted(gv_root.glob("*/vars.yml")):
        try:
            gv = yaml.safe_load(gv_path.read_text()) or {}
        except Exception:
            continue
        svcs = gv.get("traefik_services", []) or []
        if any(isinstance(x, dict) and x.get("name", "").startswith("dashboard-") for x in svcs):
            sites_with_dashboard.append(gv_path.parent.name)
    for site_grp in sites_with_dashboard:
        gv_path = repo_root / f"ops/ansible/group_vars/{site_grp}/vars.yml"
        gv = yaml.safe_load(gv_path.read_text())
        services = gv.get("traefik_services", []) or []
        dashboard_entry = next(
            (
                s
                for s in services
                if isinstance(s, dict)
                and s.get("name", "").startswith("dashboard-")
            ),
            None,
        )
        t.assert_eq(
            f"{site_grp}: dashboard service entry present",
            dashboard_entry is not None,
            True,
            finding="F-080",
        )
        if dashboard_entry is not None:
            t.assert_in(
                f"{site_grp}: middleware attached to dashboard router",
                "dashboard-root-to-access-control",
                dashboard_entry.get("middlewares", []) or [],
                finding="F-080",
            )

    return t.report()


if __name__ == "__main__":
    sys.exit(main())
