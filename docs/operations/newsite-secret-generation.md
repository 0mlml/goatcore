# Generate missing site secrets

`ops/scripts/newsite-secrets.sh` creates missing site credentials in a secret
store and writes only references to `.envrc.local.d/30-site-services.refs`.
Values do not enter local files, command arguments or console output
([ADR 1186](../adr/1186-credentials-at-rest-are-references-only.md)).

For 1Password, install and sign in to `op`, choose the vault your organisation
uses for this site's services, and set `GOAT_SECRET_VAULT` or pass `--vault`.
No vault is assumed. The generator manages a Secure Note titled
`goat site <site-id> services`, tagged `goat-newsite-secrets`; pointers use vault
and item IDs so renaming either does not invalidate them. It refuses duplicate
items, an unmanaged item with that title, or duplicate/empty stored fields.

```bash
mkdir -p .envrc.local.d
chmod 700 .envrc.local.d
ops/scripts/newsite-secrets.sh zagros-prod --dry-run
ops/scripts/newsite-secrets.sh zagros-prod --vault '<chosen vault>'
direnv allow
ops/scripts/with-secrets ops/scripts/ansible-preflight.sh \
  -i ops/ansible/inventory/mgmt-hosts-mesh.ini \
  ops/ansible/zagros-prod-oidc-clients.yml -e target=zagros_prod_hosts
```

`--file` selects a different `.refs` file in an existing directory. It no longer
accepts plaintext `.envrc` output. The generator preserves existing assignments
in its target, the repo's other `.refs` files, legacy `.envrc.local`/`*.envrc`
files, and the command environment. It does not migrate or rotate them. When
Ansible runs from a linked worktree without its own `.envrc.local.d`, the
preflight wrapper validates and loads references from the primary checkout
before resolving them. It retains already resolved inherited values and refuses
malformed reference files without printing their contents. Legacy files load
only while `GOAT_REFERENCES_ONLY` is unset or zero; use `with-secrets` to resolve
new references during that transition. A join to an existing identity
or mesh cluster must reuse its Class-B secrets before generation; this tool is
not a second cluster-genesis ceremony.

Store writes happen first, then an atomic replacement writes the complete
reference file with mode 0600. The lock prevents concurrent writers to that
file. If the reference write fails, retry: already stored fields are reused,
not regenerated. Do not run concurrent generators against the same store item
from different clones or destinations. After an interrupted process, verify
that no writer remains before removing its `<file.refs>.lock` directory.

For the team's OpenBao store (secret-store-openbao.md), set
`GOAT_SECRET_STORE=openbao`: the generator writes the values to
`goat/goat-site-services/goat-site-<site-id>-services` and the `bao://` references to
`.envrc.local.d/openbao/30-site-services.refs`. It needs a token with write on that partition (a
platform admin's `bao login -method=oidc`, or the `newsite-generator` machine identity).

For another store, set `GOAT_SECRET_STORE=custom`, `GOAT_SECRET_REF_PREFIX` to
its reference scheme, and `GOAT_SECRET_INIT` to one executable adapter path.
The adapter reads `{ "site": "...", "secrets": { "NAME": "value" } }` as JSON
on stdin, persists missing names, and returns exactly `{ "NAME": "reference" }`
as JSON on stdout. It must reuse existing values on retries; it must not echo
values to stdout or stderr. Nonzero exit, invalid JSON, missing/extra names or
non-reference results prevent the local write. References must use URI-safe
letters, digits and `:/_.@%+-`. Also configure `GOAT_SECRET_RUN` for resolution
as described in [secret store requirements](../design/secret-store-requirements.md).

## Portal seed formats

The singular `<SITE>_PORTAL_OPERATOR_API_KEY` is a bare 32-byte base64 bearer.
The plural `<SITE>_PORTAL_OPERATOR_API_KEYS` is comma-separated `handle:key`
pairs. A bare base64 value in the plural is invalid. For a new portal only,
`--bootstrap-operator <handle>` supplies an explicit named seed; the store's
`pair:<handle>` format keeps the name attached to its own 32-byte key. The
handle must name an approved bootstrap operator; no `<site>-bootstrap` admin
or shared default person is invented.

Without that option the generator supplies the legacy singular seed only;
it does not invent a person. If either portal credential family already
exists, both are retained and neither a new legacy authority nor a new named
operator is added. Repeating the bootstrap option leaves the roster unchanged
and reports that no grant was made. Subsequent human grants and revocations
use `goat admin platform-admin` and the console's two-person approval
([ADR 1182](../adr/1182-platform-admin-lifecycle-one-command.md)).

The optional plural stays in `--spec-json` as `pair:` until an explicit
handle is supplied. A default fresh run generates 42 families; an explicit
named seed adds the 43rd. Use the dry-run's delta instead of a baked total.
Both outputs are store references, never plaintext local keys. On a site
using the unprefixed role defaults, point `PORTAL_OPERATOR_API_KEY` and/or
`PORTAL_OPERATOR_API_KEYS` at the corresponding store field in a `.refs` file.

The portal role validates the resolved values before any rendering: strings,
parser-compatible pairs, and at least one nonempty singular or named key.
Empty comma entries and duplicate-handle last-wins behavior follow the Go
parser. Unresolved store references and malformed pairs fail before replacing
the container. At runtime, parser errors identify the entry number without
including its key. Existing service-key directory and revoke guards remain
in force.

The live P12 set is the role map plus the inline clients, and the secret-free
NetBird PKCE client remains optional. Preflight rejects empty, short and
unresolved secrets before the first Hydra request. The coverage guard parses
YAML mappings (comments do not count), checks what each role actually sends,
and joins those client families and their existing cookie consumers to the
generator's `--spec-json` output. The cookie contract uses 32-character hex
values, preserving existing proxy configuration. Stethoscope and goat-map have no cookie
lookup and receive no invented cookie variables.

Validation uses isolated fake stores and localhost Ansible only. A signed-in
live-store run and a deployed Hydra login remain operator acceptance work;
the automated checks do not claim that those happened.
