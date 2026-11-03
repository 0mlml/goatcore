#!/usr/bin/env python3
"""Render the real roles/spire server.conf.j2 for a sovereign's hermetic SPIRE server.

    render-spire.py <template> <trust-domain> <out> [<bundle endpoint address>]

Federation on, as a goatnet federating with a partner sets it; sqlite instead of the
goatnet's postgres, which this run does not stand up. The block under test is the
template's own, including the bundle endpoint's serving profile.
"""
import sys

import jinja2

tmpl, td, out = sys.argv[1:4]
addr = sys.argv[4] if len(sys.argv) > 4 else "0.0.0.0"
env = jinja2.Environment(undefined=jinja2.StrictUndefined, keep_trailing_newline=True)
with open(tmpl, encoding="utf-8") as fh:
    t = env.from_string(fh.read())
with open(out, "w", encoding="utf-8") as fh:
    fh.write(t.render(
        ansible_managed="rendered by ops/federation-e2e/render-spire.py",
        spire_trust_domain=td,
        spire_log_level="INFO",
        spire_ca_ttl="2160h",
        spire_default_svid_ttl="1h",
        spire_agent_svid_ttl="360h",
        spire_federation_enabled=True,
        spire_federates_with=[],
        spire_federation_bundle_endpoint_address=addr,
        spire_federation_bundle_endpoint_port=8443,
        spire_datastore_type="sqlite3",
        spire_datastore_connection_string="/opt/spire/data/server/datastore.sqlite3",
        spire_db_name="unused", spire_db_user="unused",
    ))
