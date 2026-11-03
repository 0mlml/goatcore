# Activating Ubuntu Pro, and where the token lives

**What this gets you:** `fips-updates` and `usg` on the fleet's own hardware. Neither is available
unattached, and the AWS pay-as-you-go route does not reach an on-premises machine — that
entitlement is per-instance and its kernel is `-aws-fips`.

**Before starting, know the deadline.** The free trial is **30 days, up to 50 machines, for
enterprise and commercial use**. It requires a payment method and **auto-bills at day 30 unless
cancelled**. Activating it commits someone to a date.

## 1. Activate — a person, in a browser

Not automatable and not something to hand to an agent: it creates an account and takes a payment
card.

1. Sign in or create an Ubuntu One account at <https://ubuntu.com/pro/free-trial>.
2. Start the trial. Record the **renewal date** wherever dated commitments live, not only here.
3. Copy the **contract token** from the Ubuntu Pro dashboard.

Do **not** use the free-for-personal-use tier (5 machines). It is licensed for personal projects
and this is not one. The licence is the reason, not the machine count.

## 2. Store the token

It is a third-party subscription token, so it lands in `10-cloud-and-thirdparty.envrc` alongside
the other vendor keys — see local-secrets-pattern:

```bash
export UBUNTU_PRO_TOKEN="..."
```

**It is a fleet-scope secret, not a host-scope one.** One token attaches every machine on the
subscription. And rotating it does not detach anything already attached — `pro detach` on the host
does that. Rotation is not revocation here.

## 3. Attach a host

```bash
sudo pro attach "$UBUNTU_PRO_TOKEN"
sudo pro enable fips-updates --assume-yes
sudo reboot                      # required; the FIPS kernel only takes at boot
```

## 4. Verify — on the provider, not the entitlement

`pro status` saying `enabled` is **not** evidence the crypto is validated. Measured on a real FIPS
host, `swanctl --list-algs` still reported `AES_GCM_16[aesni]` — strongSwan's own AES-NI code,
outside the module — while every obvious check said yes
(F-466).

So check all four, and treat the last as the one that matters:

```bash
cat /proc/sys/crypto/fips_enabled     # 1
uname -r                              # ...-fips
sudo pro status | grep fips-updates    # enabled
sudo swanctl --list-algs | grep AES_GCM_16   # MUST read [openssl]
```

`goat-peering-edge` sets the plugin configuration that makes the fourth true, and refuses to
proceed if it is not.

## What is still unmeasured on our hardware

Everything above was verified on **Ubuntu Pro 24.04 in AWS**, kernel `6.8.0-1063-aws-fips`. An
on-premises mini PC runs the **generic** FIPS kernel, which has not been tested here. Re-read
`swanctl --list-algs` on the first on-prem edge rather than assuming the cloud result carries over
— the plugin-selection behaviour should, since it belongs to strongSwan and the archive, but that
is a reasoned expectation and not a measurement.

**Related:** [os-release-cadence](../design/os-release-cadence.md) for which release to attach and
what each offers.
