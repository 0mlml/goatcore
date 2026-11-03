"""
Where a site's leaf certificate or key lives on the controller (#2165).

mint-site-certs.sh writes each leaf to the site manifest's `out_dir`
(~/.goat/state/<site>-leaves by convention), outside every checkout.
Plays used to read a gitignored copy staged inside the checkout
(ops/enrollment/<site>/), so a fresh worktree, which every deploy now runs
from (C3, #2124), had no leaf until the staging step was re-run.

    {{ 'router-cert.pem' | goat_leaf(site_id) }}
    {{ 'zagros/names-collector-cert.pem' | goat_leaf }}     # site/file form

The first existing file wins:
  1. $GOAT_LEAF_DIR/<file>                      an explicit override
  2. <manifest out_dir>/<file>                  the per-operator state dir
  3. <this checkout>/ops/enrollment/<site>/<file>     the old staged copy
  4. <main checkout>/ops/enrollment/<site>/<file>     the same, seen from a worktree
When none exists the state-dir path (2) is returned, so a pre-flight that
stats it names the place the leaf belongs. Keys are never read here: only
paths are computed.
"""

import os
import subprocess
from pathlib import Path

OPS = Path(__file__).resolve().parents[2]
REPO = OPS.parent


def _split(name, site):
    if site:
        return site, name
    if "/" not in name:
        raise ValueError(f"goat_leaf: '{name}' needs a site: '{name}' | goat_leaf(site_id)")
    site, _, rest = name.partition("/")
    return site, rest


def state_dir(site):
    """The manifest's out_dir, else the ~/.goat/state convention."""
    manifest = OPS / "enrollment" / "manifests" / f"{site}-certs.yaml"
    out = None
    if manifest.is_file():
        for line in manifest.read_text(encoding="utf-8").splitlines():
            if line.startswith("out_dir:"):
                out = line.split(":", 1)[1].split("#", 1)[0].strip().strip("'\"")
                break
    if not out:
        out = f"~/.goat/state/{site}-leaves"
    return Path(os.path.expanduser(out))


def _main_checkout():
    try:
        common = subprocess.run(
            ["git", "-C", str(REPO), "rev-parse", "--path-format=absolute", "--git-common-dir"],
            capture_output=True, text=True, timeout=5, check=True,
        ).stdout.strip()
    except (OSError, subprocess.SubprocessError):
        return None
    main = Path(common).parent
    return None if main == REPO else main


def candidates(name, site=None):
    site, rel = _split(name, site)
    out = []
    override = os.environ.get("GOAT_LEAF_DIR")
    if override:
        out.append(Path(os.path.expanduser(override)) / rel)
    out.append(state_dir(site) / rel)
    out.append(OPS / "enrollment" / site / rel)
    main = _main_checkout()
    if main:
        out.append(main / "ops" / "enrollment" / site / rel)
    return out


def goat_leaf(name, site=None):
    paths = candidates(name, site)
    for p in paths:
        if p.is_file():
            return str(p)
    return str(paths[1] if os.environ.get("GOAT_LEAF_DIR") else paths[0])


def goat_leaf_mint(name, site=None):
    """The command that puts a missing leaf where goat_leaf looks first."""
    site, rel = _split(name, site)
    leaf = rel.rsplit("-", 1)[0] if rel.endswith(("-cert.pem", "-key.pem")) else rel
    return f"ops/scripts/mint-site-certs.sh {site} --only {leaf}"


class FilterModule:
    def filters(self):
        return {"goat_leaf": goat_leaf, "goat_leaf_mint": goat_leaf_mint}
