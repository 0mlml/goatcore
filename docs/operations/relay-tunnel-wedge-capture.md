# Capturing a relay management-tunnel wedge

When a relay stops forwarding `:443` but the relay host is up — the F-594 class —
the fault lives in the rathole control channel between the management host and the relay, and it clears before it can be studied. This runbook takes
the capture that F-594 could not: a single isolated client under strace with a
packet capture on both ends of the same handshake.

## When to run it

`RelayEnrollmentTunnelDown` has fired for a relay, or `curl --resolve
intake.<zone>:443:<relay-ip>` returns nothing while the other relays answer 200.
Run it **while the wedge is live** — the capture is worthless after recovery.

## Run it

From the operator laptop, with the door to the goatnet open and AWS creds for
the relay account:

```bash
# door endpoint for the management host:
ops/scripts/env-door.sh zagros --ssh-target        # e.g. goat@127.0.0.1:12201
# the relay's instance + region (terraform output 'relays', or the inventory):
aws-vault exec usareur-af --no-session -- \
  ops/relay/capture-tunnel-wedge.sh <relay> \
    --thor goat@127.0.0.1:12201 \
    --instance <instance-id> --region <region> --seconds 30
```

It stops the one `rathole-client@<relay>` unit so a single process can be
straced (the `Restart=always` loop pollutes every capture otherwise), records both ends for the window, pulls the files, and
**always restarts the client on exit**, including on error or Ctrl-C.

## What you get, and the question it answers

| File | Shows |
|---|---|
| `thor-client-strace.log` | the isolated client: `connect`, the noise `write` (msg1), what it `read`s back, and the exact syscall where it closes |
| `thor-2333.pcap` | the handshake from the management host's side (wireshark: follow the `:2333` stream) |
| `relay-2333.pcap` | the same handshake from the relay's side |
| `relay-server.log` | the server's view — noise EOF vs handshake timeout vs auth failure |

The open question: the client connects and the noise handshake does not
complete (the server reads EOF) thousands of times, then recovers on its own.
This capture shows, for one isolated attempt, which step fails and whether the
server ever sends its msg2 — which decides whether the fault is the client
giving up, the server never answering, or the path dropping handshake packets.

Detection is already covered by F-596; this is how the next occurrence becomes
root-caused instead of another guess.
