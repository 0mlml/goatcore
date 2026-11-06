# goat-zenoh-portal role tests

Parse-only fixture. Real behavioral coverage lives in the Go test suite at
`ops/goat-zenoh-portal/` (`go test
./...` — registry validation, ACL render correctness, cert-mint
tarball assembly, end-to-end HTTP smoke through every API surface).

This role is a thin deployment wrapper. Its risk surface is the
template rendering (which `syntax-check.sh` covers) and the
docker-compose wiring (covered live at deploy time).
