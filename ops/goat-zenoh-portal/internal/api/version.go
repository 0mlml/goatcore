package api

import (
	"strconv"
	"strings"
)

// APIVersion is the version of the REST contract this binary serves. It is
// echoed as the Api-Version response header on every /api response and is the
// info.version the served openapi.yaml must carry (openapi_contract_test.go).
// A request that names another major version is refused.
const APIVersion = "0.2.0"

// apiMajor is the major component of APIVersion, compared against the
// Api-Version request header.
var apiMajor, _ = majorOf(APIVersion)

func majorOf(v string) (int, bool) {
	head, _, _ := strings.Cut(strings.TrimSpace(v), ".")
	n, err := strconv.Atoi(head)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// apiVersionAccepted reports whether a request's Api-Version header names a
// contract this binary serves: absent, or the same major version.
func apiVersionAccepted(requested string) bool {
	if strings.TrimSpace(requested) == "" {
		return true
	}
	major, ok := majorOf(requested)
	return ok && major == apiMajor
}
