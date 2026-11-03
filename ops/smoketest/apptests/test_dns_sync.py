"""Tests for DNS resolution of Netbird domain and relay subdomains."""

import socket


def test_domain_resolves():
    """netbird.legacy.example resolves to at least 1 IP address."""
    results = socket.getaddrinfo("netbird.legacy.example", 443, socket.AF_INET)
    ips = {r[4][0] for r in results}
    assert len(ips) >= 1, "netbird.legacy.example did not resolve to any IP"


def test_round_robin_count():
    """netbird.legacy.example resolves to exactly 3 IPs (round-robin)."""
    results = socket.getaddrinfo("netbird.legacy.example", 443, socket.AF_INET)
    ips = {r[4][0] for r in results}
    assert len(ips) == 3, f"Expected 3 round-robin IPs, got {len(ips)}: {ips}"


def test_relay_dns():
    """Each relay subdomain resolves to a distinct IP."""
    relay_subdomains = [
        "isr.netbird.legacy.example",
        "mum.netbird.legacy.example",
        "uae.netbird.legacy.example",
    ]
    resolved = {}
    for fqdn in relay_subdomains:
        results = socket.getaddrinfo(fqdn, 443, socket.AF_INET)
        ips = {r[4][0] for r in results}
        assert len(ips) >= 1, f"{fqdn} did not resolve"
        resolved[fqdn] = ips

    # Each relay should resolve to its own IP
    all_ips = [next(iter(v)) for v in resolved.values()]
    assert len(set(all_ips)) == len(all_ips), (
        f"Relay subdomains should resolve to distinct IPs: {resolved}"
    )
