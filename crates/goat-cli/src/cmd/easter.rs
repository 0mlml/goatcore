// `goat goat` — a hidden easter egg (docs/design/goat-cli-onboarding-and-delight.md).
// Prints a charming ASCII goat + version + a one-line nudge toward the
// real surfaces. Only ever runs when explicitly invoked, so it carries no
// machine surface and no `--json`; the `_complete`-style `hide = true`
// keeps it out of `goat --help` so it stays a find-it-yourself delight.

pub fn run() -> anyhow::Result<()> {
    println!("{}", crate::fun::ASCII_GOAT);
    println!(
        "goat {} — `goat tutor` to learn it, `goat doctor` to trust it.",
        env!("CARGO_PKG_VERSION")
    );
    Ok(())
}

/// `goat truths` — RFC 1925, "The Twelve Networking Truths." A deep-cut
/// nod to the early IETF; genuinely apt for a brokerless data fabric.
pub fn truths() -> anyhow::Result<()> {
    println!("{}", crate::fun::RFC_1925_TRUTHS);
    Ok(())
}

/// `goat xyzzy` — the Colossal Cave Adventure magic word. The hallowed
/// terminal easter egg; the only correct response is that nothing happens.
pub fn xyzzy() -> anyhow::Result<()> {
    println!("{}", crate::fun::XYZZY);
    Ok(())
}

/// `goat brew` — HTTP 418, "I'm a teapot" (HTCPCP, RFC 2324). The canonical
/// April-1 RFC, with the goat twist.
pub fn brew() -> anyhow::Result<()> {
    print!("{}", crate::fun::TEAPOT_418);
    Ok(())
}
