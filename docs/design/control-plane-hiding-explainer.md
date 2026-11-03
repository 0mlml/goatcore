# How GOAT-Net hides its control plane from the public internet — explainer

**Audience:** sponsors, security reviewers, new team members, partners. Assumes you understand "internet" and "DNS" at a basic level; doesn't assume WireGuard expertise.

**TL;DR:** We can't make our cloud relays invisible — they have public IP addresses, anyone can scan for them. What we DO is (1) never give the world a name pointing AT them so casual discovery via DNS produces nothing, (2) make sure that when an attacker DOES find an IP and probe it, the relay refuses to even acknowledge the probe (silence, not error responses), and (3) hand out the secrets needed to talk to a relay only through a separate trusted channel that doesn't touch the internet. Together these turn "find and attack the management plane" from a 5-minute Shodan search into something requiring physical access to a trusted operator and weeks of effort.

---

## 1. What we can't hide

Our relays are EC2 instances at AWS. AWS gives them **public IP addresses**. Those addresses are on the internet. Any port scanner — Shodan, Censys, masscan, your laptop — can sweep the whole IPv4 space and find them within hours. There is no way to remove this. That's the trade for being reachable at all.

So the question is NOT "can we be invisible?" It's "given that someone CAN find our IPs, what can they actually DO with that knowledge?"

## 2. The three layers of hiding

We hide along three distinct dimensions. Each one alone is partial; together they're substantial.

### Layer 1: discovery — no DNS pointing at us

When an ordinary service (say, `mail.gmail.com`) runs on the internet, it has a DNS record: a public phone-book entry that says "mail.gmail.com is at 142.250.x.x." Anyone can ask DNS, "what's the IP of mail.gmail.com?" and the answer comes back instantly. Two things happen as a result:

- **Discovery is automatic.** You don't need to scan; you ASK and get told.
- **DNS itself is a leak.** Even if you don't connect to the IP, the DNS query record exists on lots of intermediate servers. Adversaries can mine DNS to learn that "company X has servers at addresses Y."

We do the opposite for our control plane. We **register no public DNS** name pointing at our relays. There is no `netbird-prod.example`, no `goat-mgmt.something.example`, no nothing. If you ask DNS for our control plane, you get **NXDOMAIN** (no such name exists).

That means an attacker can't:
- Search "GOAT-Net" or "usareur-af" in DNS-search engines (Censys, Shodan, dnsdb) and get the relay IPs.
- Exfiltrate our infrastructure shape from third-party DNS resolvers' query logs.
- Use DNS-rebinding attacks (no DNS to rebind).
- Do certificate-transparency mining (no Let's Encrypt or commercial CA cert because there's no DNS name to issue one to).

What's still possible: a brute-force port scan of the entire IPv4 space. If you have weeks and bandwidth, you'll find every public IP including ours. That's where layer 2 takes over.

### Layer 2: probing — relays are silent under unauthenticated probes

When the attacker's port scanner hits our relay's public IP, a normal service would answer:
- TCP 443 (HTTPS): "TLS hello, my certificate is for `mail.gmail.com`, my server is nginx 1.18..."
- SSH 22: "SSH-2.0-OpenSSH_8.9..."
- Even closed ports usually send back a "Connection refused" (TCP RST), confirming the IP is alive.

Each of those answers is a **fingerprint** — a free clue about what's running, what version, who owns it.

Our relay's control plane runs on **UDP, not TCP**, on a non-standard port (default UDP 51821, configurable per relay). The protocol is **WireGuard**.

WireGuard has an unusual property: it **does not respond to packets from peers it doesn't know**. There is no "hello," no error, no "connection refused" equivalent. Packets without a valid cryptographic peer-key are dropped silently in the kernel — the same way the kernel handles random noise.

What an attacker sees when they probe UDP 51821 on our relay:
- They send a packet.
- Nothing comes back.
- They can't distinguish "the port is closed" from "the port is open but my key is wrong" from "the host is down."

Compared to a TLS-fronted mgmt API (the legacy pattern), this is a categorical reduction in fingerprint surface. The attacker learns essentially nothing from probing.

### Layer 3: key handoff — bundles delivered out-of-band

For a peer (a forward-deployed device) to actually USE the control plane, it needs three things:
- The relay's IP address.
- The UDP port to connect to.
- The relay's WireGuard **public key**.

The public key is the cryptographic identity the relay uses to verify the peer is allowed to talk to it. Without the right key on the peer's side, the peer's packets get dropped silently (per layer 2 above).

We deliver these three things to peers via **out-of-band bundles**. A bundle is a small encrypted file (CBOR-encoded, signed by an offline-CA YubiKey held by an authorized operator) that contains:
- The list of `(relay_ip, udp_port, relay_pubkey)` for every relay.
- The peer's own setup-key + initial WireGuard keypair.
- A short expiry (e.g. 90 days) so a stolen bundle has limited useful life.

Bundles travel via **trusted physical paths** — USB stick handed to the deployment operator, or end-to-end encrypted email between known principals — not the public internet. They do NOT travel via the relays themselves; bundle distribution is independent of the relays' existence.

Once a peer has its bundle, it constructs a WireGuard tunnel to one or more relays using the keys from the bundle. From the attacker's perspective looking at the relay externally, NOTHING about this exchange is visible — it looks like more noise on UDP 51821, indistinguishable from the legitimate traffic from other peers.

## 3. Putting the layers together — what an attacker sees vs can do

| Step in attack chain | Without hiding (legacy) | With hiding (Block 22) |
|---|---|---|
| **Find the target** | Search "goat" / "usareur-af" in DNS / cert-transparency / Shodan → get IPs in seconds | Brute-force port scan all of IPv4 → days/weeks; no name to associate with the org |
| **Confirm what's running** | TLS handshake reveals "this is netbird mgmt at organization X" | UDP probe gets no response; can't distinguish "service" from "noise" from "down" |
| **Probe for vulns** | Test the TLS stack, mgmt API, login form, etc. | No L7 endpoint exists to probe — the API lives INSIDE a WireGuard tunnel that requires keys to enter |
| **Attempt to enroll** | Hit the mgmt API's setup-key endpoint, try to brute-force it | Without an operator-issued bundle, no way to even ESTABLISH a tunnel; the API is unreachable |
| **DDoS** | Floods to TCP 443 hit our app stack, can exhaust connection pools, etc. | Floods to UDP 51821 hit WireGuard's kernel-level silent-drop (designed to be cheap) + AWS Shield Standard handles volumetric L3/L4 attacks at AWS edge |
| **Cert / DNS attacks** | Cert pinning bypass, DNS rebinding, DoH-vs-resolver discrepancies, DNSSEC poisoning | None apply — no DNS, no cert |

The total effect: an attacker who finds our IPs by brute scan still has **none of the next-step affordances** they're used to. There's no fingerprint to match against vuln databases, no API surface to enumerate, no DNS records to mine. The realistic attack path is "compromise an operator and steal a bundle" — which is a very different (and much harder) attack than "find the public mgmt URL and probe it."

## 4. What we give up

Three things we LOSE versus running with public DNS + commercial protection:

1. **Cloudflare's commercial WAF / L7 protection.** Cloudflare's main protective products require DNS records pointing at CF's edge. Without DNS, those products don't apply. We replace this with: AWS Shield Standard (auto-on for L3/L4 DDoS), WireGuard's intrinsic cheap-per-bad-packet drop in the kernel, optional nftables source-IP rate limiting. For our threat model — small relay fleet, modest peer count, sovereign-tactical use case — this is sufficient.

2. **Friction for legitimate users.** A new device can't just visit a URL and join. They need a bundle delivered through a trusted path. This is a feature for adversary resistance and a cost for ergonomics. We mitigate with a simple bundle generation + delivery workflow (offline-CA YubiKey + USB or encrypted email).

3. **Easy debugging.** Operators can't `curl https://mgmt-url/healthz` from an unconfigured laptop. They have to be on the wg-cp0 tunnel first. We mitigate with explicit operator runbooks for wg-cp0 client setup on operator laptops.

These trades are deliberate. They favor adversary-resistance over ease-of-onboarding, which matches the project's threat model.

## 5. What we still need to defend

Hiding the control plane is one defense layer. It does NOT defend against:

- **Operator compromise.** If an attacker gets bundle access via a compromised operator laptop, none of the above matters. Defense: hardware-token signing (YubiKey PIV for the offline CA), short bundle expiry, per-bundle activation deadlines.
- **Insider threat.** A trusted operator who wants to leak bundles can. Defense: dual-control / two-of-N for sensitive operations (Phase 3+ FROST signing), audit logs of every bundle issued.
- **Supply-chain compromise.** A backdoored WireGuard, kernel, or AWS AMI gets us regardless of how silent our listener is. Defense: reproducible builds (Block 40), measured boot + dm-verity (Block 18C), SLSA L4 supply chain.
- **Physical capture of a relay.** If an adversary takes possession of the AWS instance image, they get the relay's WireGuard private key. Defense: relays don't hold long-term peer secrets (only their own per-relay key), relay rotation is a config flip, peer keys are issued per-device (lose one, the others are safe).
- **Cryptographic break of WireGuard.** WireGuard is well-vetted but a future flaw would degrade silent-drop to just-dropped. Defense: defense-in-depth (mgmt-API still requires its own auth, peers still need setup-keys for actual enrollment).

## 6. "Wait — what about firewalls?" (for traditional-net-eng audiences)

A common reaction from a network engineer is: "Okay, but every server needs a firewall — what's our firewall?" The honest answer: we have three firewalls, layered. They're not replaced by Block 22; they're augmented by a fourth layer.

| Layer | What | Where | Behavior on bad packet |
|---|---|---|---|
| 1. **AWS VPC Security Group** | Cloud-provider stateful firewall, default-deny | At AWS edge, before the packet reaches the EC2 | Drop at AWS edge (no response sent back to source) |
| 2. **OS-level nftables** | In-kernel packet filter on the EC2 | On the relay itself, before any userland program sees the packet | Drop or rate-limit (drop or queue depending on config) |
| 3. **WireGuard kernel listener** | Cryptographic packet validation | After nftables, in the kernel WireGuard module | **Silent drop** — no response of any kind |
| 4. **Application logic in the mgmt-API** | Setup-key validation, ACL enforcement, etc. | Inside the wg-cp0 tunnel, only reachable after layer 3 succeeded | Application-level reject (e.g. HTTP 401) |

The traditional firewall (layer 1) is **still doing its job** — it's the cloud-provider boundary that makes the EC2 unreachable on every port except the few we deliberately opened. We did NOT remove the firewall. We added layer 3 (WireGuard's cryptographic silent-drop) on top of it.

Why layer 3 matters more than layer 1 for hiding: layer 1 (and even most layer 2 configurations) responds to "port closed" probes with TCP RST or ICMP unreachable. That tells the attacker "the IP is alive, this port is closed." Layer 3 doesn't tell them anything — packets just disappear.

If a net-eng asks "but how do you log dropped packets / detect intrusions?" — the answer is the **wg-cp0 handshake-attempt logger** (a small shim that records when WireGuard saw a packet it dropped) plus AWS VPC Flow Logs (which capture every packet flow at the AWS edge). Together they give us comparable visibility to a traditional firewall log, with the difference that the data is shipped to our self-hosted observability stack (Prometheus + OpenObserve) rather than sitting on the firewall appliance.

**Single-sentence framing**: we did NOT remove the firewall; we added a second, stronger lock that uses cryptographic identity instead of source-IP filtering, and then made it silent-by-design so port scanners learn nothing.

## 7. "What about the netbird mesh data plane on the same boxes?" (clarifying co-resident public surfaces)

A reader who's read the rest of the architecture docs may notice an apparent contradiction: the relay VPSes also host **publicly-addressable** TLS listeners on TCP 33073 (the netbird-relay binary, both the legacy Go and the new Block 33 Rust tier). How is that reconciled with "we hide our control plane"?

The reconciliation is that "control plane" and "data plane" are different things on the same boxes:

- The **control plane** (mgmt-API, signal-server federation, Spock replication between mgmt-hosts) is what this doc describes as silent. It rides the wg-cp0 surface (UDP 51821) on the legacy tier only. There is no DNS for it; the listener answers no unauthenticated probe.
- The **data plane** (netbird mesh peer-to-peer ZTOL traffic) preferentially goes peer-to-peer with no relay involvement at all. When P2P fails (CGNAT-on-both-ends), peers fall back to a **publicly addressable** TLS listener on TCP 33073 — `rels://goat-relay.example:33073` (Rust new tier) and `rels://relay-<id>.example:33073` (legacy Go tier). Those listeners *do* answer TLS handshakes pre-auth, *do* have public DNS, *are* fingerprintable.

Why the asymmetry is OK: data-plane bytes traversing the public netbird-relay are **WireGuard-encrypted by the peers** at the inner layer. The relay is a blind opaque forwarder; it cannot decrypt the traffic. So fingerprinting the public TCP 33073 listener tells an attacker "this is a netbird relay" — useful for targeting it (DDoS, exhausting its connection-pool) but not useful for breaking the encrypted payload, and not useful for reaching the management plane (which is on the silent surface and requires bundle-rooted keys).

The two surfaces share physical infrastructure but have different threat models. The silent posture (this doc) protects mgmt-plane discovery + targeting + exploitation. The data-plane TCP 33073 surface accepts a higher exposure for the operational reality that CGNAT-on-both-ends pairings need a public rendezvous, and counters with WireGuard-end-to-end-encryption + Block 33 perf hardening + AWS Shield Standard.

For the full path-by-path picture (which path rides which listener on which tier), see `relay-paths.md`.

## 8. The one-paragraph summary, again

**We can't be invisible — but we can be uninteresting.** No DNS name announces us. The only visible fingerprint is a UDP port that drops your packets without acknowledgment. The keys you'd need to actually CONNECT come from a trusted operator's USB stick, not from our relays. An attacker who finds our IPs has done the easy part of an attack chain that no longer has an obvious next step.

## Cross-references

- `docs/design/relay-paths.md` — single-page summary of the silent vs public surfaces on the relay tier and which path rides which
- `docs/design/wg-cp0-prod.md` — concrete implementation (topology, address plan, key model, bundle format)
- ADR 0313 — silent control plane via outer WireGuard wrap
- ADR 0314 — no public DNS for control plane
- ADR 0315 — peer-key attestation via offline CA (deeper trust posture for the per-device wg-cp0 keys)
- ADR 0308 — offline enrollment CBOR bundles (the bundle format Layer 3 uses)
- [`docs/design/offline-enrollment.md`](offline-enrollment.md) — full design of the bundle workflow (provisioning, validation, revocation, key ceremony)
