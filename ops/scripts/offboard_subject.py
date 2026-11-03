#!/usr/bin/env python3
"""Resolve offboarding inputs from the manifests native revocation consumes."""

import argparse
import re
import sys
from pathlib import Path

import yaml


class Refusal(ValueError):
    """The local registry cannot identify one person safely."""

    def __init__(self, message, *, github_login=None, github_source="unresolved"):
        super().__init__(message)
        self.github_login = github_login
        self.github_source = github_source


class UniqueLoader(yaml.SafeLoader):
    """Reject duplicate mapping keys instead of silently choosing an identity."""

    def construct_mapping(self, node, deep=False):
        keys = [self.construct_object(key, deep=deep) for key, _ in node.value]
        try:
            if len(set(keys)) != len(keys):
                raise Refusal("duplicate YAML mapping key")
        except TypeError as exc:
            raise Refusal("non-scalar YAML mapping key") from exc
        return super().construct_mapping(node, deep=deep)


# Match YAML 1.2 boolean spelling: valid handles such as "on" are strings.
UniqueLoader.yaml_implicit_resolvers = {
    key: [(tag, pattern) for tag, pattern in rules
          if tag != "tag:yaml.org,2002:bool"]
    for key, rules in yaml.SafeLoader.yaml_implicit_resolvers.items()
}
UniqueLoader.add_implicit_resolver(
    "tag:yaml.org,2002:bool", re.compile(r"^(?:true|True|TRUE|false|False|FALSE)$"),
    list("tTfF"),
)


def slug(value):
    return isinstance(value, str) and re.fullmatch(r"[a-z0-9][a-z0-9_-]*", value)


def email(value):
    return (isinstance(value, str) and not value.startswith("-")
            and re.fullmatch(r"[^@\s\x00-\x1f\x7f]+@[^@\s\x00-\x1f\x7f]+", value))


def email_key(value):
    """Match native eq_ignore_ascii_case without folding Unicode identities."""
    return value.translate(str.maketrans("ABCDEFGHIJKLMNOPQRSTUVWXYZ",
                                        "abcdefghijklmnopqrstuvwxyz"))


def github_login(value, label="identity.github"):
    """Validate the bare login shape used by the onboarding path."""
    if value is None:
        return None
    if not isinstance(value, str):
        raise Refusal(f"invalid {label}")
    login = value.strip()
    if (not login or len(login) > 39
            or not all(char.isascii() and (char.isalnum() or char == "-")
                       for char in login)
            or login.startswith("-")):
        raise Refusal(f"invalid {label}")
    return login


def read_manifest(path):
    if path.is_symlink() or not path.is_file():
        raise Refusal(f"{path.name}: expected a regular manifest file")
    try:
        data = yaml.load(path.read_text(encoding="utf-8"), Loader=UniqueLoader)
    except (OSError, UnicodeError, yaml.YAMLError, Refusal) as exc:
        # YAML diagnostics can quote the document: never echo manifest contents.
        raise Refusal(f"{path.name}: unreadable or invalid YAML manifest") from exc
    identity = data.get("identity") if isinstance(data, dict) else None
    if not isinstance(identity, dict):
        raise Refusal(f"{path.name}: missing identity mapping")
    handle, login = identity.get("handle"), identity.get("email")
    if not slug(handle) or handle != path.stem:
        raise Refusal(f"{path.name}: identity.handle must match the filename")
    if not email(login):
        raise Refusal(f"{path.name}: invalid identity.email")
    return handle, login, identity.get("github")


def resolve(operators, subject, goatnets):
    """Return (goatnet, handle, email, GitHub login) rows after validation."""
    if not (slug(subject) or email(subject)):
        raise Refusal("subject must be a handle or login email")
    rows = []
    for goatnet in dict.fromkeys(goatnets):
        if not slug(goatnet):
            raise Refusal("invalid goatnet name")
        directory = operators / goatnet
        if directory.is_symlink() or not directory.is_dir():
            raise Refusal(f"{goatnet}: missing regular operator manifest directory")
        try:
            manifests = [read_manifest(path) for path in sorted(directory.glob("*.yml"))]
        except OSError as exc:
            raise Refusal(f"{goatnet}: cannot read operator manifest directory") from exc
        matches = [(handle, login, github) for handle, login, github in manifests
                   if (email_key(login) == email_key(subject) if "@" in subject else handle == subject)]
        if len(matches) != 1:
            raise Refusal(f"{goatnet}: subject must match exactly one operator manifest")
        handle, login, github = matches[0]
        try:
            github = github_login(github)
        except Refusal as exc:
            raise Refusal(f"{handle}.yml: invalid identity.github") from exc
        if sum(email_key(other) == email_key(login) for _, other, _ in manifests) != 1:
            raise Refusal(f"{goatnet}: login email maps to multiple handles")
        if rows and email_key(login) != email_key(rows[0][2]):
            raise Refusal("selected goatnets map the subject to different login emails")
        rows.append((goatnet, handle, login, github))
    if not rows:
        raise Refusal("select at least one goatnet")
    return rows


def github_login_owner(operators, login, subject_email):
    """Return the handle of another person whose manifest names this login."""
    try:
        paths = sorted(operators.glob("*/*.yml"))
    except OSError as exc:
        raise Refusal("cannot read operator manifests to check the GitHub login") from exc
    for path in paths:
        try:
            data = yaml.load(path.read_text(encoding="utf-8"), Loader=UniqueLoader)
        except (OSError, UnicodeError, yaml.YAMLError, Refusal) as exc:
            # An unreadable manifest could name this login: refuse rather than guess.
            raise Refusal(f"{path.parent.name}/{path.name}: unreadable manifest; "
                          "cannot prove the GitHub login belongs to no one else") from exc
        identity = data.get("identity") if isinstance(data, dict) else None
        if not isinstance(identity, dict):
            continue
        other, owner_email = identity.get("github"), identity.get("email")
        if (isinstance(other, str) and other.strip().casefold() == login.casefold()
                and not (isinstance(owner_email, str)
                         and email_key(owner_email) == email_key(subject_email))):
            return identity.get("handle") or path.stem
    return None


def resolve_github(rows, supplied, allow_unverified, operators):
    """Bind one GitHub login to the selected manifests, or refuse."""
    supplied_login = github_login(supplied, "--github-user") if supplied else None
    named = {}
    for goatnet, _, _, login in rows:
        if login is not None:
            named.setdefault(login.casefold(), []).append((goatnet, login))
    if len(named) > 1:
        mappings = ", ".join(
            f"{goatnet}={login}"
            for entries in named.values() for goatnet, login in entries
        )
        raise Refusal(
            f"selected goatnets name different identity.github logins: {mappings}",
            github_login=supplied_login,
        )
    if named:
        entries = next(iter(named.values()))
        expected = entries[0][1]
        if supplied_login and supplied_login.casefold() != expected.casefold():
            raise Refusal(
                f"--github-user {supplied_login} does not match manifest identity.github {expected}",
                github_login=expected,
                github_source="manifest",
            )
        return expected, "manifest"
    if not supplied_login or not allow_unverified:
        raise Refusal(
            "no selected manifest names identity.github; pass --github-user and "
            "--github-user-unverified to acknowledge the unbound login",
            github_login=supplied_login,
        )
    owner = github_login_owner(operators, supplied_login, rows[0][2])
    if owner:
        raise Refusal(
            f"--github-user {supplied_login} is identity.github of {owner}; "
            "an unverified login must not belong to another person",
            github_login=supplied_login,
        )
    return supplied_login, "operator-unverified"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--operators-dir", type=Path, required=True)
    parser.add_argument("--subject", required=True)
    parser.add_argument("--goatnet", action="append", required=True)
    args = parser.parse_args()
    try:
        rows = resolve(args.operators_dir, args.subject, args.goatnet)
    except Refusal as exc:
        print(f"offboard: REFUSE — {exc}", file=sys.stderr)
        return 1
    for row in rows:
        print("\t".join(value or "" for value in row))
    return 0


if __name__ == "__main__":
    sys.exit(main())
