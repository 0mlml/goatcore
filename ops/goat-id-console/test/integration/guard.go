package integration

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
)

// Every endpoint the suite can reach a server on. KRATOS_PUBLIC_URL belongs
// here too: the TOTP seeder drives the self-service login and settings flows,
// which live on the public port. Leaving it out would put a real egress path
// outside the pre-flight — the same gap that made an earlier version of this
// guard decorative.
var requiredAdminURLVars = []string{
	"IDP_TENANT_KRATOS_ADMIN_URL",
	"IDP_TENANT_HYDRA_ADMIN_URL",
	"IDP_OPERATOR_KRATOS_ADMIN_URL",
	"IDP_OPERATOR_HYDRA_ADMIN_URL",
	"KRATOS_PUBLIC_URL",
}

// RequireLoopback enforces that every endpoint handed to it resolves to a
// loopback-only host: it parses each URL and requires a scheme, a non-empty
// host, and a hostname that is either `localhost` or an IP inside the loopback
// ranges (127.0.0.0/8, ::1). A hostname is refused rather than resolved, so DNS
// can never talk this guard into approving a deployed goatnet.
//
// It validates exactly the map it is given. An earlier version took this map
// and then ignored it, iterating a hardcoded list instead — so a caller that
// passed a public endpoint under any other name had it silently unchecked,
// which is the same vacuous-guard failure this file exists to prevent. Callers
// that need the four IdP admin URLs enforced should use RequireLoopbackEnv.
func RequireLoopback(vars map[string]string) error {
	if len(vars) == 0 {
		return fmt.Errorf("no endpoints supplied to the loopback guard — refusing rather than passing vacuously")
	}
	for _, key := range sortedKeys(vars) {
		value := vars[key]
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("missing required var: %s", key)
		}

		u, err := url.Parse(value)
		if err != nil {
			return fmt.Errorf("invalid url for %s: %w", key, err)
		}

		if u.Scheme == "" {
			return fmt.Errorf("missing scheme for %s", key)
		}

		host := u.Hostname()
		if host == "" {
			return fmt.Errorf("missing host for %s", key)
		}

		if !isLoopbackHost(host) {
			return fmt.Errorf("non-loopback host for %s: %s", key, host)
		}
	}

	return nil
}

// RequireLoopbackEnv is the CI pre-flight: it reads the console's four IdP
// admin URL variables from the process environment, refuses if any is unset,
// and then applies the loopback check. Fail-closed on absence is the point —
// a guard that iterated zero variables and returned nil would be indis-
// tinguishable from one that passed.
func RequireLoopbackEnv() error {
	vars := make(map[string]string, len(requiredAdminURLVars))
	for _, key := range requiredAdminURLVars {
		v := os.Getenv(key)
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("missing required var: %s", key)
		}
		vars[key] = v
	}
	return RequireLoopback(vars)
}

// sortedKeys keeps the error a caller sees deterministic when several
// variables are wrong.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return ip.IsLoopback()
}
