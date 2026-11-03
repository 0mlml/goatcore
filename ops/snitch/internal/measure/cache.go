// Package measure — cross-cycle caching for network fingerprinting.
//
// Background. The review (deliverable B) called out the
// gateway / public-IP / mDNS sub-probes as "deprioritize per cycle":
// gateway hardware doesn't change without a MAC change, public IP
// changes correlate with subnet flips, mDNS results depend on the
// interface. Re-running them every 60s burns budget for almost no
// signal.
//
// Strategy. A small in-process cache keyed on the change-trigger for
// each field. The probe loop instantiates a CachingNetworkFunc once
// and passes it via WithNetwork. The cache survives across cycles in
// the same process; restarts re-prime it (which is fine — startup is
// the natural time to do a full sweep).
//
// Discipline:
//   - Cache misses fall back to the full DetectNetwork. Same surface,
//     same struct shape, no behavior change visible to callers.
//   - Hard TTL on public IP (6h) is the safety net: if our change-
//     detection misses a subnet flip, we still re-probe within 6h.
//   - This is local-only state; never persists to disk. Snitch's
//     analytics layer is the source of truth for "what we observed."

package measure

import (
	"context"
	"sync"
	"time"

	"github.com/usareur-af/goatcore/ops/snitch/internal/types"
)

// NetworkCache holds cross-cycle state for network fingerprinting.
// Goroutine-safe (the probe loop is single-threaded today, but this
// future-proofs against a parallel-probe refactor).
type NetworkCache struct {
	mu sync.Mutex

	last           types.NetworkFingerprint
	hasLast        bool
	lastInterface  string
	lastGatewayMAC string
	lastSubnet     string
	publicIPAt     time.Time

	// PublicIPTTL is the maximum age of a cached public IP / ASN /
	// ASNOrg before forcing a refresh. Default 6h per the
	// review. Tests can override.
	PublicIPTTL time.Duration
}

// NewNetworkCache returns an empty cache with default TTLs.
func NewNetworkCache() *NetworkCache {
	return &NetworkCache{PublicIPTTL: 6 * time.Hour}
}

// CachingNetwork returns a NetworkFunc that consults the cache and
// only re-runs the expensive sub-probes when their change-triggers
// fire. The "cheap" sub-probes (interface detection, gateway IP,
// gateway RTT, subnet) re-run every cycle — they're already fast and
// they ARE the change-detection signal for the rest.
//
// Trigger logic per field:
//   - GatewayInfo (HTTP banner / mDNS): MAC changed
//   - PublicIP / ASN / ASNOrg: TTL elapsed OR subnet changed
//
// Pure orchestration; the underlying detect functions are unchanged.
func (c *NetworkCache) CachingNetwork() NetworkFunc {
	return func(ctx context.Context) types.NetworkFingerprint {
		// Always re-detect the cheap fields — these are the signals we
		// use to decide whether to refresh the expensive ones.
		fp := types.NetworkFingerprint{}
		iface, ifType := detectInterface(ctx)
		fp.Interface = iface
		fp.InterfaceType = ifType
		if ifType == "wifi" {
			fp.SSID = detectSSID(ctx, iface)
		}
		fp.GatewayIP = detectGateway(ctx)
		if fp.GatewayIP != "" {
			if result, err := Ping(ctx, fp.GatewayIP, 3); err == nil {
				fp.GatewayRTT = result.AvgMs
			}
		}
		fp.Subnet = DetectSubnet(ctx, iface)

		// Gateway MAC + manufacturer — passive ARP read, ~ms cost. We
		// re-run this every cycle so we can detect MAC change without
		// a full HTTP/mDNS sweep.
		var currentMAC string
		if fp.GatewayIP != "" {
			currentMAC = lookupARP(ctx, fp.GatewayIP)
		}

		c.mu.Lock()
		defer c.mu.Unlock()

		// Decide whether the gateway-hardware sub-probe (HTTP banner
		// + mDNS — the expensive bits of DetectGateway) needs to run.
		gatewayChanged := currentMAC != c.lastGatewayMAC || iface != c.lastInterface
		if gatewayChanged || !c.hasLast {
			fp.Gateway = DetectGateway(ctx, fp.GatewayIP)
		} else {
			// Reuse cached gateway info; keep the freshly-read MAC.
			fp.Gateway = c.last.Gateway
			if currentMAC != "" {
				fp.Gateway.MAC = currentMAC
				fp.Gateway.Manufacturer = ouiLookup(currentMAC)
			}
		}

		// Public IP / ASN — TTL OR subnet change triggers refresh.
		subnetChanged := fp.Subnet != c.lastSubnet && c.lastSubnet != ""
		ttlExpired := time.Since(c.publicIPAt) > c.PublicIPTTL
		if !c.hasLast || subnetChanged || ttlExpired {
			pub, asn, org := detectPublicIP(ctx)
			fp.PublicIP = pub
			fp.ASN = asn
			fp.ASNOrg = org
			if pub != "" {
				c.publicIPAt = time.Now()
			}
		} else {
			fp.PublicIP = c.last.PublicIP
			fp.ASN = c.last.ASN
			fp.ASNOrg = c.last.ASNOrg
		}

		c.last = fp
		c.hasLast = true
		c.lastInterface = iface
		c.lastGatewayMAC = currentMAC
		c.lastSubnet = fp.Subnet
		return fp
	}
}
