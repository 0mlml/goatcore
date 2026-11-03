# goatbuilder

`goatbuilder` is the build/release command-line for a goat environment. You
declare a release in the source repository, it converges the environment you
name, and it reports done only from each machine's own deploy record.

This book is built from the same tag as the binary you downloaded, so what it
says is what that binary does. The version badge at the bottom right of every
page names the tag.

| Chapter | Read it when |
|---|---|
| **First run** | you are about to deploy or release with it for the first time: `doctor`, a dry run, the live run, the proof |
| **Command reference** | you need a flag; it is the binary's own `--help`, verb by verb |
| **The design** | you want to know why it works the way it does: the model, the deploy contract C1–C10, the converger interface, the migration from the shell drivers |
| **The contract as tests** | you are admitting a verb, a transport or an environment kind, and need the acceptance |
| **Distribution and hosting** | you are putting the binary somewhere: the public release, this goatnet's download surface, an islanded site |

The binary came from one of two places, with one signature: the public
release page on the source repository, or this goatnet's download surface
beside this book. Verify the signature over `SHA256SUMS` before you install,
from either.
