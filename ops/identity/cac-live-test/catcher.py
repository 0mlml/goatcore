#!/usr/bin/env python3
"""catcher.py — a tiny host-run OIDC relying party for testing the CAC paths with a
REAL CAC in a browser. Runs on the host (reaches the IdP via its published localhost
port, same as the browser), does the PKCE code exchange SERVER-SIDE (no CORS), and
displays the id_token claims your CAC produced — the EDIPI highlighted.

Env: CAC_ISSUER (e.g. https://localhost:8843/realms/goat), CAC_CLIENT (default catcher).
Run indirectly via ./run.sh keycloak|mtls <dod-ca-bundle.pem>. Listens on :9090.
Skips TLS verify because the IdP server cert is self-signed in the sandbox (this is the
RP<->IdP back-channel, not the CAC trust — that is validated by the IdP against the CA).
"""
import base64, hashlib, html, http.server, json, os, secrets, ssl, urllib.parse, urllib.request

ISSUER = os.environ.get("CAC_ISSUER", "").rstrip("/")
CLIENT = os.environ.get("CAC_CLIENT", "catcher")
REDIRECT = "http://localhost:9090/cb"
# MODE=token  -> catcher is the OIDC RP straight to the CAC front-end (shows the token).
# MODE=kratos -> full chain: send the browser through Kratos (which federates to the CAC
#                front-end) and show the minted native goat identity.
MODE = os.environ.get("CATCHER_MODE", "token")
KRATOS_PUBLIC = os.environ.get("KRATOS_PUBLIC", "http://localhost:4433").rstrip("/")
KRATOS_ADMIN = os.environ.get("KRATOS_ADMIN", "http://localhost:4434").rstrip("/")
CTX = ssl.create_default_context(); CTX.check_hostname = False; CTX.verify_mode = ssl.CERT_NONE
_verifiers = {}

def disco():
    with urllib.request.urlopen(ISSUER + "/.well-known/openid-configuration", context=CTX) as r:
        return json.load(r)

def b64url(b): return base64.urlsafe_b64encode(b).rstrip(b"=").decode()

def page(body): return ("<!doctype html><meta charset=utf-8><title>goat CAC live test</title>"
    "<style>body{font:15px/1.5 -apple-system,Segoe UI,Roboto,sans-serif;max-width:760px;margin:40px auto;padding:0 16px}"
    ".btn{background:#1b5e20;color:#fff;padding:10px 18px;border-radius:6px;text-decoration:none}"
    ".edipi{background:#e8f5e9;border:1px solid #1b5e20;border-radius:6px;padding:12px 16px;font-size:22px;margin:16px 0}"
    "pre{background:#f5f5f5;border:1px solid #ddd;border-radius:6px;padding:12px;overflow:auto}"
    ".err{background:#ffebee;border:1px solid #c62828;color:#b71c1c;padding:12px;border-radius:6px}"
    ".muted{color:#666;font-size:13px}</style>" + body)

class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def _send(self, body, code=200):
        b = body.encode(); self.send_response(code)
        self.send_header("Content-Type", "text/html; charset=utf-8"); self.send_header("Content-Length", str(len(b)))
        self.end_headers(); self.wfile.write(b)
    def do_GET(self):
        u = urllib.parse.urlparse(self.path); q = urllib.parse.parse_qs(u.query)
        # --- full-chain mode: browser goes through Kratos; show the minted goat identity ---
        if MODE == "kratos" and u.path == "/":
            reg = KRATOS_PUBLIC + "/self-service/registration/browser?return_to=http://localhost:9090/done"
            return self._send(page('<h1>goat — CAC → goat identity (full chain)</h1>'
                '<p>Plug in your CAC, then start the flow. You go through <b>Kratos</b>, which '
                'federates to the CAC front-end (your card + PIN), then mints your native goat identity.</p>'
                f'<p><a class=btn href="{html.escape(reg)}">Start: register/login with CAC →</a></p>'
                '<p class=muted>Accept the self-signed server-cert warning + pick your CAC when prompted. '
                'On the Kratos page, choose the <b>CAC</b> option.</p>'))
        if MODE == "kratos" and u.path == "/done":
            try:
                with urllib.request.urlopen(KRATOS_ADMIN + "/admin/identities?per_page=50", context=CTX) as r:
                    ids = json.load(r)
            except Exception as e:
                return self._send(page(f'<div class=err>could not read identities: {html.escape(str(e))}</div>'))
            fed = [i for i in ids if (i.get("metadata_public") or {}).get("external_subjects")]
            if not fed:
                return self._send(page('<div class=err>No federated goat identity found yet. '
                    'Did the CAC login complete? <a href="/">try again</a></div>'))
            i = sorted(fed, key=lambda x: x.get("created_at",""))[-1]
            es = i["metadata_public"]["external_subjects"][0]
            return self._send(page('<h1>goat — CAC → goat identity ✓</h1>'
                f'<div class=edipi>Native goat identity minted from your CAC<br>'
                f'anchor (Kratos UUID): <b>{html.escape(i["id"])}</b><br>EDIPI: <b>{html.escape(str(es.get("subject")))}</b></div>'
                '<p>traits:</p><pre>'+html.escape(json.dumps(i.get("traits"),indent=2))+'</pre>'
                '<p>metadata_public.external_subjects (the CAC binding + assurance):</p><pre>'
                +html.escape(json.dumps(i["metadata_public"]["external_subjects"],indent=2))+'</pre>'
                '<p><a class=btn href="/">test again</a></p>'))
        if u.path == "/":
            try: d = disco()
            except Exception as e:
                return self._send(page(f'<div class=err>Cannot reach the IdP discovery at {html.escape(ISSUER)} — is it up?<br>{html.escape(str(e))}</div>'))
            state = secrets.token_urlsafe(16); ver = b64url(secrets.token_bytes(32))
            _verifiers[state] = ver
            chal = b64url(hashlib.sha256(ver.encode()).digest())
            auth = d["authorization_endpoint"] + "?" + urllib.parse.urlencode({
                "client_id": CLIENT, "response_type": "code", "scope": "openid email profile",
                "redirect_uri": REDIRECT, "state": state, "code_challenge": chal, "code_challenge_method": "S256"})
            return self._send(page(f'<h1>goat — CAC live test</h1><p class=muted>IdP: {html.escape(ISSUER)} • client: {CLIENT}</p>'
                f'<p>Plug in your CAC, then:</p><p><a class=btn href="{html.escape(auth)}">Login with CAC →</a></p>'
                '<p class=muted>Your browser prompts for the certificate + PIN when it reaches the IdP. '
                'If it warns about the self-signed <b>server</b> cert, accept/proceed first.</p>'))
        if u.path == "/cb":
            code = q.get("code", [""])[0]; state = q.get("state", [""])[0]
            ver = _verifiers.pop(state, "")
            if not code: return self._send(page(f'<div class=err>no code returned: {html.escape(u.query)}</div>'))
            try:
                d = disco()
                body = urllib.parse.urlencode({"grant_type": "authorization_code", "code": code,
                    "redirect_uri": REDIRECT, "client_id": CLIENT, "code_verifier": ver}).encode()
                req = urllib.request.Request(d["token_endpoint"], data=body,
                    headers={"Content-Type": "application/x-www-form-urlencoded"})
                with urllib.request.urlopen(req, context=CTX) as r: tok = json.load(r)
            except Exception as e:
                return self._send(page(f'<div class=err>token exchange failed: {html.escape(str(e))}</div>'))
            if tok.get("error"): return self._send(page(f'<div class=err>token error: {tok.get("error")} — {html.escape(tok.get("error_description",""))}</div>'))
            idt = tok.get("id_token", "")
            try: claims = json.loads(base64.urlsafe_b64decode(idt.split(".")[1] + "=="))
            except Exception: claims = {}
            edipi = claims.get("edipi") or claims.get("preferred_username") or claims.get("sub") or "(none)"
            return self._send(page(f'<h1>goat — CAC live test</h1>'
                f'<div class=edipi>EDIPI extracted from your real CAC: <b>{html.escape(str(edipi))}</b></div>'
                f'<p>Full id_token claims your CAC produced:</p><pre>{html.escape(json.dumps(claims, indent=2))}</pre>'
                '<p><a class=btn href="/">test again</a></p>'))
        return self._send(page('<div class=err>not found</div>'), 404)

if __name__ == "__main__":
    if not ISSUER: raise SystemExit("set CAC_ISSUER")
    print(f"catcher on http://localhost:9090  (IdP {ISSUER}, client {CLIENT})")
    http.server.HTTPServer(("0.0.0.0", 9090), H).serve_forever()
