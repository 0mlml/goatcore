#!/usr/bin/env python3
"""test_render.py — the icam-mtls-proxy templates render from the registry for the
default profile list, and the rendered nginx.conf is one nginx accepts.

Renders nginx.conf.j2, docker-compose.yml.j2 and dex.yaml.j2 with plain Jinja2, the
role's sovereign_pki_* filters and a stand-in for Ansible's regex_replace, using the
role defaults. With an nginx binary on PATH, runs `nginx -t` on the result (the
`load_module` line is dropped unless NGINX_JS_MODULE or the module file is present).
Run: python3 ops/ansible/roles/icam-mtls-proxy/test_render.py
"""
import importlib.util
import os
import pathlib
import re
import shutil
import subprocess
import tempfile
import unittest

import yaml
from jinja2 import Environment, FileSystemLoader, StrictUndefined, select_autoescape

ROLE = pathlib.Path(__file__).resolve().parent
REPO = ROLE.parents[3]
FILTERS = REPO / "ops/ansible/filter_plugins/sovereign_pki.py"
REGISTRY = REPO / "ops/identity/sovereign-pkis.yaml"


def load_filters():
    spec = importlib.util.spec_from_file_location("sovereign_pki", FILTERS)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod.FilterModule().filters(), mod


def render_all(pkis=("us-dod",)):
    filters, mod = load_filters()
    # nginx / compose / Dex configs, not HTML: .j2 is not in the autoescape set, as in Ansible.
    env = Environment(loader=FileSystemLoader(str(ROLE / "templates")), undefined=StrictUndefined,
                      keep_trailing_newline=True, autoescape=select_autoescape(["html", "xml"]))
    env.filters.update(filters)
    env.filters["regex_replace"] = lambda s, pat, rep: re.sub(pat, rep.replace("\\\\1", "\\1"), s)
    defaults = yaml.safe_load((ROLE / "defaults/main.yml").read_text())
    ctx = dict(defaults, site_id="zagros", site_dns_zone="netbird.example.net", playbook_dir="/x")
    ctx["icam_mtls_proxy_issuer_url"] = "https://card-proxy.zagros.netbird.example.net"
    ctx["icam_mtls_proxy_pkis"] = list(pkis)
    registry = yaml.safe_load(REGISTRY.read_text(encoding="utf-8"))
    ctx["icam_sovereign_pkis"] = filters["sovereign_pki_select"](registry, ctx["icam_mtls_proxy_pkis"])
    return {name: env.get_template(name).render(**ctx) for name in ("nginx.conf.j2", "docker-compose.yml.j2", "dex.yaml.j2")}


class RenderTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.out = render_all()

    def test_nginx_conf_carries_the_registry_maps_and_the_two_headers(self):
        ng = self.out["nginx.conf.j2"]
        self.assertIn("load_module modules/ngx_http_js_module.so;", ng)
        self.assertIn("js_import pki from /etc/nginx/pki.js;", ng)
        self.assertNotIn("pki-profiles.js", ng)                     # no san_othername profile enabled
        self.assertIn("js_set $pki_san_upn pki.sanUpn;", ng)
        self.assertIn('"~,OU=PKI,OU=DoD,O=U\\.S\\. Government,C=US$" us-dod;', ng)
        self.assertIn("proxy_set_header X-Remote-User  $pki_remote_user;", ng)
        self.assertIn("proxy_set_header X-Remote-Pki   $pki_id;", ng)
        self.assertIn("server_name card-proxy.zagros.netbird.example.net;", ng)
        self.assertNotIn("edipi", ng.lower())                         # nothing PKI-specific in the template

    def test_compose_mounts_pki_js_and_the_merged_trust_file(self):
        dc = yaml.safe_load(self.out["docker-compose.yml.j2"])
        vols = dc["services"]["nginx"]["volumes"]
        self.assertIn("./pki.js:/etc/nginx/pki.js:ro", vols)
        self.assertIn("./certs/ca-bundle.pem:/certs/ca.crt:ro", vols)
        self.assertNotIn("./pki-profiles.js:/etc/nginx/pki-profiles.js:ro", vols)

    def test_dex_connector_is_the_generic_card_connector(self):
        dex = yaml.safe_load(self.out["dex.yaml.j2"].replace("${ICAM_MTLS_KRATOS_CLIENT_SECRET}", "x"))
        self.assertEqual(dex["connectors"][0]["id"], "card")
        self.assertEqual(dex["connectors"][0]["config"]["userHeader"], "X-Remote-User")
        self.assertTrue(dex["staticClients"][0]["redirectURIs"][0].endswith("/callback/card-local"))

    def test_a_sketch_profile_in_the_list_fails_the_render(self):
        with self.assertRaises(ValueError):
            render_all(("us-dod", "est-id"))

    @unittest.skipUnless(shutil.which("nginx"), "nginx is needed for nginx -t")
    def test_rendered_nginx_conf_passes_nginx_t(self):
        ng = self.out["nginx.conf.j2"]
        module = os.environ.get("NGINX_JS_MODULE") or next(
            (m for m in ("/usr/lib/nginx/modules/ngx_http_js_module.so", "/etc/nginx/modules/ngx_http_js_module.so") if os.path.exists(m)), "")
        tmp = pathlib.Path(tempfile.mkdtemp(prefix="icam-mtls-proxy-render-"))
        try:
            (tmp / "logs").mkdir()
            subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", str(tmp / "proxy.key"),
                            "-out", str(tmp / "proxy.crt"), "-days", "1", "-subj", "/CN=t"], check=True,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            shutil.copy(tmp / "proxy.crt", tmp / "ca.crt")
            shutil.copy(ROLE / "files/pki.js", tmp / "pki.js")
            conf = ng.replace("/certs/", str(tmp) + "/").replace("/etc/nginx/pki.js", str(tmp / "pki.js"))
            if module:
                conf = conf.replace("load_module modules/ngx_http_js_module.so;", f"load_module {module};")
            else:
                conf = re.sub(r"^load_module .*\n|^\s*js_import .*\n|^\s*js_set .*\n", "", conf, flags=re.M)
                conf = conf.replace("http {", 'http {\n    map $host $pki_san_upn { default ""; }\n    map $host $pki_san_emails { default ""; }', 1)
            conf = conf.replace("listen 8443 ssl;", "listen 127.0.0.1:18443 ssl;").replace("http://dex:5556", "http://127.0.0.1:5556")
            # pid + error_log in the temp dir and -e: a packaged nginx otherwise opens
            # /run/nginx.pid and /var/log/nginx/error.log, which a test user cannot.
            conf = f"pid {tmp / 'nginx.pid'};\nerror_log {tmp / 'logs' / 'error.log'};\n" + conf
            conf = conf.replace("http {", "http {\n    access_log off;", 1)   # the packaged default is /var/log/nginx/access.log
            (tmp / "nginx.conf").write_text(conf)
            r = subprocess.run(["nginx", "-e", str(tmp / "logs" / "error.log"), "-t", "-c", str(tmp / "nginx.conf"), "-p", str(tmp)],
                               capture_output=True, text=True)
            self.assertEqual(r.returncode, 0, r.stderr)
        finally:
            shutil.rmtree(tmp, ignore_errors=True)


if __name__ == "__main__":
    unittest.main()
