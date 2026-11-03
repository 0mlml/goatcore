# retro-notice — reusable retroactive-notice mailer

Reference implementation + batch driver for the general onboarding-email formula
([docs/design/onboarding-email-formula.md](../../../../docs/design/onboarding-email-formula.md)):
email an already-onboarded person about something new (a permission change, a
feature, a migration, an update), customized deterministically by role.

## Run

Roster TSV columns: `name<TAB>email<TAB>groups-csv<TAB>whitelisted`, where
`groups-csv` is `traits.groups` from Kratos (e.g. `PLATFORM-ADMIN,IAM-ADMIN` or
`(none)`). Build it from `/admin/identities` filtered to `state=active`.

```
python3 render.py roster.tsv rendered.json notice.relay-health.json
```

Emits `[{name,email,role,deliverable,subject,body}, ...]`. `deliverable=false`
marks reserved-TLD synthetic identities (`.test`/`.example`) — reported, not
silently dropped.

## New campaign

Copy `notice.relay-health.json`, edit the reason block (`subject`, `headline`,
`what`, `url`, `howto`, `why`, optional `why_admin`). The renderer and the role
vocabulary are unchanged — that is the point.

## Send

Deterministic render first, review, then send via SES v2 from
the goatnet's verified sender, one message per
deliverable recipient. Example:

```
# per recipient: aws sesv2 send-email --region <region> --cli-input-json file://<payload>
```

## PII

`roster.tsv`, `rendered.json`, and any send results carry operator-roster PII —
keep them out of git. Only this tool + the notice templates are committed.
