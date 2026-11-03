# Troubleshooting

When something's wrong, start with one command:

```text
goat doctor
```

It runs four steps in order and, for any that fails, prints a specific fix. Here's how to read it.

## Reading `goat doctor`

| Step | What it checks | If it fails |
|---|---|---|
| **mesh** | You're on the fabric's network | You're off-mesh — bring up your tunnel (`netbird status` / re-enroll) |
| **router** | The fabric's router is reachable | The router may be down, or your endpoint is wrong — re-check your profile |
| **certificate** | Your identity is accepted | Your cert may be expired or rejected — `goat profile show`, then re-onboard if needed |
| **portal** | Your access token works | Token expired or revoked — ask to be re-onboarded |

Each failed step exits with a distinct code, so a wrapper script can branch without parsing text.

## Common situations

**"off the ZTOL — not connected to the mesh"**
You're not on the fabric's network. Bring up your mesh tunnel and try again; `goat doctor` will confirm once you're on.

**"mTLS cert expired or rejected by the router"**
Your identity is stale. `goat profile show` to see its expiry; if it's past, you'll need a fresh bundle.

**"portal rejected the Bearer token"**
Your access token is no longer valid (expired or revoked). Ask your administrator to re-onboard you.

**`goat status` shows a `[✗]`**
`status` is the one-glance view; run `goat doctor` for the stepped detail and the specific remedy.

**`goat status` shows `ztol` green but `session` red**
The router port accepts connections, but a Zenoh session does not open: the router is wedged, or it rejects your certificate. A mesh bounce does not fix this. Run `goat doctor` and tell your administrator.

## Understanding an error code

Every error carries a stable code (like `E_OFF_MESH`). To look one up:

```text
goat explain E_OFF_MESH
```

## Practice without risk

If you just want to explore commands without touching anything real, the sandbox is always there:

```text
goat hello --sandbox
goat tutor
```

## Still stuck?

Capture the exact command and its output (use `--json` for a clean, complete record) and bring it to your goat administrator.
