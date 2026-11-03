"""Shared fixtures for smoketest application tests."""

import os

import pytest
import requests
import urllib3

# Suppress InsecureRequestWarning when TLS verification is disabled
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)


@pytest.fixture
def api_base_url():
    """Base URL for the Netbird management API."""
    return os.environ.get("NETBIRD_DOMAIN", "https://netbird.legacy.example")


@pytest.fixture
def api_token():
    """Netbird API token from environment."""
    token = os.environ.get("NETBIRD_API_TOKEN")
    if not token:
        pytest.skip("NETBIRD_API_TOKEN not set")
    return token


@pytest.fixture
def http_session(api_token):
    """Requests session with auth header and TLS verification disabled."""
    session = requests.Session()
    session.headers.update({
        "Authorization": f"Token {api_token}",
        "Accept": "application/json",
    })
    session.verify = False
    return session
