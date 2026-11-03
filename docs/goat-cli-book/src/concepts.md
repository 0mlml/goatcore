# How the fabric works

You can use goat productively with just three ideas: **identity**, **topics**, and **no broker**. Here they are.

## A mesh you join with an identity

The fabric is a self-healing mesh. Peers connect **directly** to each other over encrypted WireGuard tunnels — once you're enrolled, your data doesn't route through a central server, and your connections survive a management-plane outage or a flaky uplink.

You join with an **identity**: an onboarding *bundle* that carries your certificate, an access token, and where the fabric lives. The fabric admits you by *who you are*, not by a shared password. `goat ca add <bundle-ca.pem>` tells goat which CA to trust — it ships trusting none — and `goat profile init <bundle.cbor>` then installs that identity; you can hold several under different *contexts* and switch between them.

```text
            you ──┐         ┌── another peer
                  │  mesh   │
   a service ─────┼─( P2P )─┼──── a sensor
                  │         │
            you ──┘         └── a partner
```

## Topics: publish and subscribe

Data moves on **topics** — slash-delimited paths like `acme/radar1/tracks`. The model is publish/subscribe:

- **Publish** (`goat pub <topic> <payload>`) writes a value to a topic.
- **Subscribe** (`goat sub <topic>`) receives every value published there, live, as long as you're allowed to read it.

A topic is just a name; you create it (`goat topic create`), publish to it, and anyone permitted can subscribe. `goat hello` is simply a publish and a subscribe wired together so you can watch a value make the round trip.

## No broker — and why that matters

There is **no central broker** sitting in the middle of the data path. When you publish, the value flows over the mesh directly to the peers subscribed to it. That's deliberate: it removes the single bottleneck and single point of failure that a broker would be.

A practical consequence: **reads are broad, writes are the cut.** Being able to *read* a topic is permissive by design; *writing* to one is governed by your identity. So you'll rarely be surprised by what you can subscribe to, and the integrity of what's published is anchored to who published it.

## Sandbox vs. real

Everything above has a faithful **in-process simulation** — the sandbox. `goat hello --sandbox`, `goat init --sandbox`, and `goat tutor` exercise the same shapes (a topic, a publish, a round-trip, a verdict) with no network and no identity, so you can learn the model before you touch the real fabric. The sandbox always says so with a `◇ SANDBOX` banner, and it never writes to or reads from a real mesh.

When you switch to a real env, the *commands don't change* — only the backing does.

> Want the deeper architecture (the relay fallback, the access model, the audit trail)? Ask your goat administrator for the design references — this guide stays focused on operating the CLI.
