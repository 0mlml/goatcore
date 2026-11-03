#!/usr/bin/env python3
"""RENDER the alertmanager templates and assert the pager severity survives it.

Static checks on a Jinja template cannot see what it produces. Two bugs got
past a green CI in three days because of that, and they failed in opposite
directions:

  1. `severity: >-` — a folded block scalar. YAML folded the COMMENTS THAT
     FOLLOWED into the value, so the rendered severity was the Go template plus
     prose. PagerDuty rejected every page with HTTP 400 while the template file
     read correctly and alertmanager reported the alerts routed. Live on
     zagros-prod for three days: 3,456 rejected notifications.

  2. Fixing (1) by hand dropped the `{% raw %}` wrapper, so ansible's Jinja
     tried to interpret the Go template's `{{ if eq ... }}` and the play died
     with "expected token 'end of print statement', got 'eq'". That one failed
     LOUD and applied nothing, which is the good direction — but CI passed it
     just the same, because nothing rendered the file.

The lesson both share: the artifact that matters is the RENDERED config, and
until this script existed nothing in CI ever produced one.

What it asserts, per pagerduty_configs block:
  - the template renders at all (catches a stripped {% raw %})
  - the output is valid YAML
  - `severity` is a single line (catches a folded scalar)
  - `severity` contains no `#` (catches a comment folded into the value)
  - the Go template survived intact rather than being evaluated by Jinja

Exit 0 clean, 1 a defect, 2 a source could not be read.
"""

from __future__ import annotations

import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
TEMPLATE_DIRS = [
    REPO / "ops/ansible/roles/obs-host/templates",
    REPO / "ops/ansible/roles/observability/templates",
]


def to_bool(v) -> bool:
    return str(v).lower() in ("true", "yes", "1", "on")


def main() -> int:
    try:
        import yaml
        from jinja2 import Environment, FileSystemLoader
    except ImportError as e:
        print(f"check-alertmanager-renders: missing dependency: {e}", file=sys.stderr)
        return 2

    # Minimal vars: enough to exercise the pagerduty branch. Anything unset
    # renders empty, which is the "nothing configured" path and also worth
    # rendering without exploding.
    vars_ = dict(
        pagerduty_integration_key="TEST",
        deadman_webhook_url="http://example/",
        deadman_webhook_token="t",
        site_id="ci",
        obs_site="ci",
    )

    failed = False
    checked = 0
    for tdir in TEMPLATE_DIRS:
        tpl = tdir / "alertmanager.yml.j2"
        if not tpl.exists():
            continue
        # ANSIBLE'S JINJA SETTINGS, NOT JINJA'S DEFAULTS. ansible renders
        # templates with trim_blocks=True, which removes the newline directly
        # after a block tag such as {% endraw %}. That is the whole mechanism of
        # the bug this script exists for: the comment line following the
        # template got pulled UP onto the same line, landing inside the folded
        # scalar. Rendering with jinja's defaults produces a CLEAN value and
        # proves nothing — verified, this script passed the known-bad template
        # until these two flags were set.
        env = Environment(loader=FileSystemLoader(str(tdir)),
                          trim_blocks=True, lstrip_blocks=False)
        # Ansible filters jinja2 does not ship. Stubbed rather than imported so
        # this runs without ansible installed.
        env.filters["bool"] = to_bool
        env.filters["regex_escape"] = lambda s: s
        try:
            out = env.get_template("alertmanager.yml.j2").render(**vars_)
        except Exception as e:  # noqa: BLE001 — any render failure is the finding
            failed = True
            print(f"TEMPLATE DOES NOT RENDER: {tpl.relative_to(REPO)}")
            print(f"  {type(e).__name__}: {e}")
            print("  A Go template inside a Jinja file must be wrapped in {% raw %}.")
            print()
            continue
        try:
            doc = yaml.safe_load(out)
        except Exception as e:  # noqa: BLE001
            failed = True
            print(f"RENDERED OUTPUT IS NOT VALID YAML: {tpl.relative_to(REPO)}")
            print(f"  {e}")
            print()
            continue

        for r in (doc or {}).get("receivers", []) or []:
            for pd in (r.get("pagerduty_configs") or []):
                checked += 1
                sev = pd.get("severity", "")
                where = f"{tpl.relative_to(REPO)} :: receiver {r.get('name')}"
                if "\n" in sev:
                    failed = True
                    print(f"SEVERITY IS MULTILINE: {where}")
                    print("  A folded/literal block scalar absorbs the lines after it.")
                    print()
                if "#" in sev:
                    failed = True
                    print(f"A COMMENT WAS FOLDED INTO THE SEVERITY VALUE: {where}")
                    print(f"  value: {sev[:160]}")
                    print("  PagerDuty rejects this with HTTP 400 and does not retry.")
                    print()
                if "{{" not in sev:
                    failed = True
                    print(f"THE GO TEMPLATE DID NOT SURVIVE RENDERING: {where}")
                    print(f"  value: {sev[:160]}")
                    print("  Jinja evaluated it; wrap it in {% raw %}.")
                    print()

    if checked == 0:
        print("check-alertmanager-renders: NO pagerduty_configs rendered", file=sys.stderr)
        print("  refusing to report clean on an empty read.", file=sys.stderr)
        return 2
    if failed:
        return 1
    print(f"check-alertmanager-renders: PASS — {checked} pagerduty block(s) render "
          f"to a clean single-line severity")
    return 0


if __name__ == "__main__":
    sys.exit(main())
