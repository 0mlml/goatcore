// `Mutator` trait — every mutating subcommand routes through `dispatch`
// so `--dry-run` is structurally impossible to forget. Per
// docs/design/goat-cli.md §Conventions adopted from modern-CLI research
// item 4.
//
// Phase B has no mutators (profile init is local-only and isn't a
// portal/Zenoh mutator). Phase C's `topic create`, `topic delete`,
// `context rm` (and any later mutator) implement this trait. The
// trait + dispatcher lands now so retrofitting later isn't necessary.
//
// Shape:
//
//   impl Mutator for CreateTopic {
//       type Output = TopicCreated;
//       fn describe_plan(&self) -> PlannedRequest { ... }
//       async fn execute(&self) -> anyhow::Result<TopicCreated> { ... }
//   }
//
//   let m = CreateTopic { ... };
//   mutator::dispatch(&m, args.dry_run).await?;

use std::fmt;
use std::future::Future;

/// What a mutator would do — rendered when `--dry-run` is set. The
/// `method` + `target` pair stays stable across releases; the `body`
/// is the operator-readable summary (not necessarily the on-wire body
/// verbatim).
#[derive(Debug)]
pub struct PlannedRequest {
    pub method: &'static str,
    pub target: String,
    pub body: String,
}

impl fmt::Display for PlannedRequest {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        writeln!(f, "would: {} {}", self.method, self.target)?;
        if !self.body.is_empty() {
            for line in self.body.lines() {
                writeln!(f, "  {line}")?;
            }
        }
        Ok(())
    }
}

pub trait Mutator {
    type Output;
    fn describe_plan(&self) -> PlannedRequest;
    fn execute(&self) -> impl Future<Output = anyhow::Result<Self::Output>> + Send;
}

/// One-shot dispatcher. Every mutator caller flows through here so
/// `--dry-run` cannot be silently ignored.
#[allow(dead_code)] // Phase C is the first consumer; lands in Phase B for shape.
pub async fn dispatch<M>(m: &M, dry_run: bool) -> anyhow::Result<Option<M::Output>>
where
    M: Mutator + Sync,
{
    if dry_run {
        print!("{}", m.describe_plan());
        Ok(None)
    } else {
        Ok(Some(m.execute().await?))
    }
}
