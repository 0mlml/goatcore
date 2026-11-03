# ADR 0836 — Matrix push gateway (hedwig) with named iOS-via-FCM-proxy waiver

| Field | Value |
|---|---|
| Status | Proposed (**amended 2026-05-07** post-Block-70.A spike — see §"Amendment" at top) |
| Date | 2026-05-07 |
| Repo scope | goat core |
| Related | [ADR 0835](0835-secure-messaging-substrate-tuwunel.md) (secure messaging substrate — sibling), [ADR 0110](0110-license-posture-agplv3-forked-components.md) (AGPL at process boundary — applies), [`../design/secure-messaging.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/secure-messaging.md), `../project/block-70-verification-spike.md` (spike report that surfaced the amendment) |

## Amendment 2026-05-07 — hedwig is NOT FCM-only

**Source-code review during Block 70.A verification spike** (see
`../project/block-70-verification-spike.md`
§3.1) found that the original framing of this ADR — based on the
hedwig README description "uses Firebase Cloud Messaging to push
messages to Android and iOS devices" — was misleading. **Hedwig has
two complete push paths: direct APNs via the `a2` Rust crate
(`src/apns.rs`) AND FCM via `firebae-cm` (`src/fcm.rs`).** Routing
is **per-device** via `NotificationMethod::Apns | Fcm` in
`src/api.rs::matrix_push`.

**What this changes in the decision below:**

1. **Tier 3 is now "hedwig + direct APNs (Apple-only in path)"** —
   not "hedwig + FCM-proxied APNs (Google + Apple in path)". The
   third-party-metadata-exposure profile is the same as Sygnal
   would have provided.
2. **Named iOS waiver scope shrinks** to the structural reality that
   Apple's APNs is unavoidable for any iOS push (this is an iOS
   platform constraint, not a hedwig decision).
3. **Block 70.D.5 v3+ upstream-contribution goal is retired** —
   the APNs-direct path it proposed to add is already in upstream.
4. **Operator deployment posture must explicitly configure hedwig
   with an Apple `.p8` private key + `team_id` + `key_id`** to enable
   the direct-APNs path. The `apns_sender` is `Option<>` — null by
   default; absent that config, iOS push falls through to the FCM
   path which IS the "FCM-proxied APNs" Google-in-path scenario.
   **This is now an explicit operator-configuration requirement, not
   an architecture limitation.**

The §Decision text below is preserved as authored 2026-05-07
morning; substantive corrections live in this Amendment block. A
full Decision rewrite lands when 70H promotes ADR 0836 from
Proposed → Accepted post-soak (Block 70.H.2). Until then, read the
Amendment as the authoritative posture.

## TL;DR (for a layperson)

Mobile chat apps cannot wake themselves up in the background;
iOS and Android force every push notification through Apple's
APNs and Google's FCM respectively. A "Matrix push gateway"
sits between our homeserver and Apple/Google to translate.
Sygnal is the canonical implementation but is Python — out per
the no-Python-for-load-bearing rule. **hedwig** (Famedly's
Rust push gateway) is the in-language alternative. It is
**FCM-only**, which means Android push goes via Google directly
(the standard Android path) and **iOS push goes via Google's
FCM-to-APNs proxy**, putting Google in the iOS path that Sygnal
would have kept Apple-only. We accept that tradeoff with an
explicit metadata-leak waiver and queue an APNs-direct upstream
contribution as the long-term path. UnifiedPush + self-hosted
ntfy is preserved as the **default** Android path, sovereign
and Google-free; hedwig is the fallback for stock-Element-from-
Play-Store users + the only path for iOS.

## Context

Sibling [ADR 0835](0835-secure-messaging-substrate-tuwunel.md)
adopts tuwunel as the Matrix homeserver. Mobile push is the
operationally trickiest piece of any Matrix deployment because
of the OS-mediated push monopolies on iOS (APNs only) and
stock Android (FCM by default).

The **homeserver itself does not talk APNs/FCM directly** — the
Matrix protocol's push design uses an intermediate gateway
service that implements the Matrix Push Gateway API and
translates to APNs/FCM. The homeserver pushes via HTTP to a
gateway URL; the gateway handles the platform-specific
delivery.

Our threat model has the standing invariant: **no part of the
secure plane resident on the public internet**. APNs and FCM
violate this by definition — their endpoints live at Apple
and Google respectively. Three sovereignty postures are
available:

1. **Pure UnifiedPush** — bypass APNs/FCM entirely via a
   community standard that lets the user pick a self-hosted
   push provider. Works on Android with FluffyChat or sideloaded
   Element. **Cannot work on iOS** — Apple structurally
   forbids third-party long-lived background connections. iOS
   users have web-only / no push.
2. **Sygnal pass-through with opaque body** — homeserver →
   Sygnal → APNs (iOS) / FCM (Android). Message body is opaque;
   metadata (who-pinged-whom, when, how often) leaks to Apple
   and/or Google. Stock Element supports this. **But Sygnal
   is Python.**
3. **hedwig pass-through with opaque body** — homeserver →
   hedwig → FCM → phone. **FCM-only:** for iOS, Google
   acts as a proxy to APNs (Apple's developer cert is uploaded
   to FCM, Google forwards on the developer's behalf). Same
   metadata-leak as Sygnal **plus Google now in the iOS path
   that Sygnal would have kept Apple-only.** Rust, in-language.

The choice is not "sovereign vs not" — sovereign means
UnifiedPush, which costs us iOS push entirely. The choice is
**how to handle the platforms where sovereignty is structurally
foreclosed.**

## Decision

Three-tier push posture:

### Tier 1 — Android via UnifiedPush (default, sovereign)

- Self-hosted **ntfy** (already named in Block 48J
  for SRE escalation; same substrate, same per-site server).
- Distributor app on the operator's phone (`ntfy` Android app
  or any UnifiedPush distributor) maintains a long-lived
  connection to our ntfy server.
- Homeserver pushes to the per-user UnifiedPush endpoint URL
  via HTTP. **No hedwig in this path. No Apple/Google in this
  path.**
- Client side: **FluffyChat** is the mature UnifiedPush-native
  Matrix client. Sideload via F-Droid or MDM. Element X
  also gained UnifiedPush support; deploy whichever the
  operator population can install.

This is the **preferred** Android path, applied as default
where the device population permits.

### Tier 2 — Android via FCM (fallback for stock Element)

- Operators on stock Element from Play Store cannot easily
  switch to UnifiedPush without sideload. For this population:
  **hedwig** translates Matrix push events to FCM.
- Same opaque-body discipline — the FCM payload is just
  `{"event_id":"$abc","room_id":"!xyz"}`; the app pulls the
  body from the homeserver after the wake-up.
- **Google in the path. Metadata leak named.** Same as the
  iOS waiver below; wait, distinguishing: on Android the
  *stock-Play-Store* user is voluntarily already in Google's
  ecosystem so the marginal sovereignty loss is bounded.

### Tier 3 — iOS via hedwig + FCM-proxied APNs

- iOS has no UnifiedPush option. APNs is the only platform-
  level push mechanism.
- **hedwig is FCM-only**; for iOS, hedwig pushes to FCM,
  which Google forwards to APNs. Both Google and Apple are in
  the iOS push path.
- Same opaque-body discipline; only the wake-up signal goes
  through Google + Apple.
- **Named waiver.** Documented metadata leak, named compensating
  controls (E2EE message bodies, no PII in room/user IDs at
  the homeserver naming layer, opaque-body push payload),
  expiry conditions (Apple opens alternative paths under
  EU DMA / iOS becomes UnifiedPush-capable / we ship a custom
  iOS client / we drop iOS).

### Long-term: APNs-direct upstream contribution to hedwig

Hedwig is small Rust (~3 years of development, single-team-
backed). Adding direct-APNs support — bypassing Google's FCM
proxy on iOS — is a bounded contribution: APNs uses HTTP/2 +
Apple-signed JWT, both of which have mature Rust libraries.
Estimated 1-2 weeks engineering. Two paths:

- **Upstream**: open a PR to famedly/hedwig with optional-APNs-
  direct mode. If accepted, sovereignty improves on iOS
  (Apple stays the only third-party in the path; Google
  removed). This is the preferred path.
- **Fork**: if the upstream PR is not merged, carry a small
  fork per [ADR 0110](0110-license-posture-agplv3-forked-components.md)
  process-boundary fork pattern.

Sequence: ship hedwig FCM-only at v2 with the iOS waiver;
attempt upstream PR after v2 ships; carry the waiver until
APNs-direct is in production. Removing Google from the iOS
path retires the waiver.

### Distributable client matrix

| Platform | Client | Push | Sovereignty |
|---|---|---|---|
| Web (any OS) | Element web (served by Caddy at `chat.<site>...`) | Browser-tab notifications when open; no push when closed | Sovereign — no third party |
| Android (preferred) | FluffyChat / Element X via F-Droid or MDM | UnifiedPush → self-hosted ntfy | Sovereign — no Google |
| Android (fallback) | Stock Element from Play Store | FCM via hedwig | Google in path; opaque body |
| iOS | Stock Element from App Store | FCM-proxied APNs via hedwig | Google + Apple in path; opaque body; **named waiver** |
| Desktop (Linux/macOS/Windows) | Element Desktop | Same as web; persistent process | Sovereign |

## Alternatives considered

- **Sygnal (Python).** Canonical Matrix push gateway. Direct
  APNs (cleaner iOS sovereignty than hedwig) but Python — out
  per the standing rule.
- **No iOS push at all (pure UnifiedPush + iOS web-only).**
  Maximally sovereign. Acceptable if the operator population
  is Android-issued. **Named as the fallback** if the iOS
  waiver is later judged unacceptable. Per-site operator-
  population decision; default for v1 is "support iOS push
  via the waiver" because forced web-only is a real
  degradation for tactical operators.
- **Build `goat-pusher` in Go.** ~800-1500 LOC of Go to
  implement the Matrix Push Gateway API direct to APNs + FCM.
  Bounded but real: ~1-2 weeks engineering, plus ongoing
  maintenance of a service that isn't our value-add. Rejected
  in favor of hedwig + upstream contribution; build-our-own
  is the v3+ option if hedwig fails or upstream rejects the
  APNs-direct PR and the fork burden becomes unwelcome.
- **Run Sygnal under explicit Python exception.** Reasoned-
  about because Sygnal is genuinely thin (stateless protocol
  fan-out, no DB, no scaling concerns at our user count, benign
  failure mode). Rejected because the no-Python rule is a
  standing posture and the in-language alternative (hedwig)
  exists and is acceptable.
- **Element X with sliding-sync and bypassing traditional
  push.** Element X improves the client side but does not
  remove APNs from the iOS path; it still requires a push
  gateway. Not a substitute for this decision.

## Consequences

### Positive

- **No Python in our stack** — passes the rule.
- **Tier 1 path (Android UnifiedPush) is fully sovereign** —
  zero hyperscaler in the path for the preferred Android
  population.
- **Hedwig is Apache-2.0 — wait, AGPL-3.0** (verified at decision
  time). Process-boundary AGPL is fine per
  [ADR 0110](0110-license-posture-agplv3-forked-components.md);
  same posture as our patched netbird-mgmt. Documented in the
  license audit register.
- **Single Rust binary** to operate per site; small
  operational surface.
- **Reuses existing per-site ntfy substrate** — no new servers
  for Tier 1.

### Negative

- **iOS metadata leak to both Google and Apple** — strictly
  worse than Sygnal's Apple-only metadata leak on iOS. Mitigated
  by the APNs-direct upstream PR plan but acceptance must be
  explicit.
- **Three push paths to operate** (UnifiedPush via ntfy / hedwig
  for Android stock / hedwig for iOS) instead of one. More
  configuration surface per operator-device-class.
- **Hedwig is small-community** (12 stars at decision time;
  one company sponsoring full-time). Carries a project-
  continuity risk lower than Sygnal's matrix.org-funded
  posture but partly mitigated by the in-language fork option
  per ADR 0110.
- **Client distribution is non-trivial on Android** if Tier 1
  via FluffyChat / sideloaded Element is the default —
  operators need F-Droid or MDM. Doable but adds onboarding
  friction.

## Assessor view (SCA-R)

This ADR serves SC-8 (transmission confidentiality — TLS 1.3
to FCM/APNs; opaque-body push payloads; message E2EE per
[ADR 0835](0835-secure-messaging-substrate-tuwunel.md)),
SC-13 (cryptographic protection), and AU-2 (audit — push
gateway events captured by Block 27 OTLP wide-event surface).

The named iOS waiver carries documented
compensating controls, named expiry conditions,
operator-acceptance signature.

## Trigger conditions for revisit

- **Hedwig project changes substantially** (sponsor withdrawal,
  abandonment, license change). Then build `goat-pusher` per
  the alternatives list, or pivot to UnifiedPush-only
  (web-only-iOS posture).
- **APNs-direct upstream PR lands** (or the fork ships in
  production). The iOS waiver retires; sovereignty improves
  by one layer (Google removed from iOS path); ADR amendment.
- **Apple opens alternative push paths** under EU DMA or
  similar. Re-evaluate the iOS approach; possibly ship a
  UnifiedPush-on-iOS path if Apple ever permits one.
- **Operator population shifts** (e.g. all-Android-issued
  fleet). Drop iOS support entirely; retire Tier 3; retire
  the waiver. Or conversely, all-iOS-issued: drop Tier 1
  (no UnifiedPush use case) and only run Tier 3.
- **Tuwunel ships a built-in pusher.** Some Matrix homeservers
  embed push functionality. Tuwunel today does not, but if
  this changes the gateway service goes away entirely.

---

*See `GLOSSARY.md` for terms and acronyms.*
