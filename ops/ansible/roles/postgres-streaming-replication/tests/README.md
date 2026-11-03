# postgres-streaming-replication role tests

Parse-only smoke. Streaming-replication behavior + the five Block 22L
gotchas (pg-major-version client image alignment, find -delete vs
rm -rf glob, chown 26:26 post-basebackup, application_name in
primary_conninfo, watchdog stop during basebackup) are exercised by the
per-site `site-bootstrap.yml` Phase 6A on real two-Orin sites — see
F-059
and F-062.

Run: `./syntax-check.sh`.
