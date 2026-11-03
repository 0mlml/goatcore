# compose-transport-one-sided — one party willing to connect is not a link

`xaa` carries `transport_offer`. `xab` does not. Both carry a full `data_offer` and `cut`.

§7: *"`transport` is emitted **only if both** packages carry `transport_offer`. Otherwise the
whole member is absent — one party willing to connect is not a link."* Until this fixture that
sentence was untested: `compose-full` has both, `compose-no-transport` has neither, and no
fixture had one.

## Expected outcome

An agreement with **no `transport` member** and two flows — and the bytes are **identical to
`../compose-no-transport/`**, digest `sha256:974778f7…`. That is not a coincidence to be
tidied away; it is the rule. One party offering a transport must produce exactly what neither
offering produces, because a link needs both halves and there is no half-link to record.

The two fixtures differ in their **inputs**, which is where the test lives.

## Two wrong rules this kills

| Wrong rule | What it does here |
|---|---|
| §6 step 4 armed on **either** package carrying `transport_offer`, not both | Runs the overlap and listener checks. `xab` has no `transport_offer`, so a lenient implementation reads its `capabilities.transports` anyway, finds an overlap, sees `xaa`'s `listener: true`, passes — and reaches §7 believing a link exists |
| `transport` emitted from the one party that has an offer | An agreement carrying `kind`, `link_subnet` and `mtu` from `xaa` alone, and `listener: "xaa"` — a complete-looking transport block describing a link the counterparty never offered |

The second is the dangerous one. It produces a valid-looking agreement that both parties would
have to compare digests to catch, and only if the counterparty got it right.

## Digest

`sha256:974778f7eece5bbb65dc33755c12cb99a35e4dbe180fbe16eaeba87ff06c4ce8`
