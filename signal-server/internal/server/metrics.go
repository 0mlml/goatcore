package server

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Outcome label values. Used across relay paths (Send, ConnectStream inbound,
// federation Forward) to keep metric labels consistent.
const (
	outcomeDelivered       = "delivered"
	outcomeForwarded       = "forwarded"
	outcomeNotFound        = "notfound"
	outcomeOutboundFull    = "outbound_full"
	outcomeInvalidArg      = "invalid_arg"
	outcomeForwardRPCError = "rpc_error"
	// outcomeForwardFailed — local miss AND the federation broadcast did NOT get
	// a definitive answer from every peer (an RPC error, timeout, breaker skip, or
	// budget cut). Delivery status is UNKNOWN: the recipient may well be online on
	// an unreachable shard. This is the F-265 case, and it MUST be distinct from
	// `notfound` (a peer definitively absent everywhere). Conflating the two is
	// exactly what made the cross-shard outage undiagnosable — the
	// dashboards read "misses" (looks like offline peers) while federation was
	// silently broken. The client-visible Send result is still OK (F-105 health-
	// probe contract preserved); the signal lives in this outcome label + log.
	outcomeForwardFailed = "forward_failed"
)

// Path label values — the code path a delivery attempt came through.
const (
	pathUnarySend = "unary_send"
	pathStream    = "stream"
)

// metrics bundles the Prometheus collectors that the server updates. One
// instance per Server, bound to its Node label at construction time.
//
// The Registerer-as-constructor argument pattern keeps metrics testable:
// tests pass a fresh `prometheus.NewRegistry()` and gather against it, no
// global state is touched.
type metrics struct {
	relayTotal           *prometheus.CounterVec
	relayShedTotal       *prometheus.CounterVec
	deliverDuration      *prometheus.HistogramVec
	messageBytes         *prometheus.HistogramVec // F-338: no size metric existed at all
	forwardSentTotal     *prometheus.CounterVec
	forwardReceivedTotal *prometheus.CounterVec
	forwardDuration      *prometheus.HistogramVec
	presencePushTotal    *prometheus.CounterVec // ADR 1108: outbound presence pushes by result
	presenceKeys         prometheus.Gauge       // ADR 1108: remote peer keys known via presence
	presenceRouteTotal   *prometheus.CounterVec // ADR 1108: cross-shard forwards by route (direct|broadcast)
}

func newMetrics(reg prometheus.Registerer, node string, r *registry) *metrics {
	labels := prometheus.Labels{"node": node}

	// Live gauge of peers currently registered. GaugeFunc reads from the
	// registry at scrape time — always accurate, no inc/dec bookkeeping
	// required in register / unregister paths.
	connectedPeers := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        "signal_server_connected_peers",
			Help:        "Number of peers currently registered at this signal server.",
			ConstLabels: labels,
		},
		func() float64 { return float64(r.count()) },
	)
	reg.MustRegister(connectedPeers)

	relayTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "signal_server_relay_total",
			Help:        "Total delivery attempts (Send RPC + ConnectStream inbound), labeled by outcome and code path.",
			ConstLabels: labels,
		},
		[]string{"outcome", "path"},
	)
	reg.MustRegister(relayTotal)

	// ADR 1081 L2 — relay attempts short-circuited by the negative cache
	// before any registry deliver or federation fan-out. Distinct from
	// relayTotal{outcome="notfound"}: a shed is a KNOWN-offline recipient we
	// declined to re-probe, not a fresh miss. Sum of the two is the true
	// miss rate to an offline peer.
	relayShedTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "signal_server_relay_shed_total",
			Help:        "Relay attempts short-circuited by the recipient-offline negative cache, labeled by code path (ADR 1081 L2).",
			ConstLabels: labels,
		},
		[]string{"path"},
	)
	reg.MustRegister(relayShedTotal)

	deliverDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "signal_server_deliver_duration_seconds",
			Help:        "Wall time of a local deliver attempt (registry lookup + channel enqueue).",
			Buckets:     prometheus.ExponentialBuckets(0.0001, 2, 12), // ~100µs to ~400ms
			ConstLabels: labels,
		},
		[]string{"outcome", "path"},
	)
	reg.MustRegister(deliverDuration)

	// MESSAGE SIZE. Previously this server had no byte-size metric of any
	// kind -- every signal_server_* series was a count -- so a relay plane that
	// was 99.3% one oversized field looked exactly like a busy one.
	//
	// The distribution that made this necessary is BIMODAL and the mean
	// describes nothing real: 93% of messages are ~260 bytes and carry 0.7% of
	// the bytes, while ~7% are ~524 KB and carry 99.3%. An average over that is
	// a number no message has. The buckets below are chosen so those two modes
	// land in DIFFERENT buckets and the split is visible without arithmetic --
	// which a mean, or a naive linear bucket set, would hide again.
	//
	// Labeled by outcome and path so "what is being shed" is answerable by
	// BYTES rather than by count: shedding 60% of messages sounds like heavy
	// protection and saves nothing if the shed ones are the small ones.
	messageBytes := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "signal_server_message_bytes",
			Help:        "Wire size of an inbound signal message, by outcome and path. Bimodal by nature -- read the buckets, never the mean.",
			Buckets:     []float64{128, 256, 512, 1024, 4096, 65536, 262144, 524288, 1048576},
			ConstLabels: labels,
		},
		[]string{"outcome", "path"},
	)
	reg.MustRegister(messageBytes)

	forwardSentTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "signal_server_forward_sent_total",
			Help:        "Outbound Forward RPC attempts from this server to peer signal servers, labeled by outcome and peer address.",
			ConstLabels: labels,
		},
		[]string{"outcome", "peer"},
	)
	reg.MustRegister(forwardSentTotal)

	forwardReceivedTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "signal_server_forward_received_total",
			Help:        "Inbound Forward RPC calls received from peer signal servers, labeled by outcome of local delivery.",
			ConstLabels: labels,
		},
		[]string{"outcome"},
	)
	reg.MustRegister(forwardReceivedTotal)

	forwardDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "signal_server_forward_duration_seconds",
			Help:        "Round-trip latency of outbound Forward RPCs to peer signal servers.",
			Buckets:     prometheus.ExponentialBuckets(0.001, 2, 12), // ~1ms to ~4s
			ConstLabels: labels,
		},
		[]string{"outcome", "peer"},
	)
	reg.MustRegister(forwardDuration)

	// ADR 1108 — presence-sync observability.
	presencePushTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "signal_server_presence_push_total",
			Help:        "Outbound presence-snapshot pushes to federation peers, by result.",
			ConstLabels: labels,
		},
		[]string{"result"},
	)
	reg.MustRegister(presencePushTotal)

	presenceKeys := prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "signal_server_presence_keys",
		Help:        "Remote peer keys currently known to be owned by a federation peer (via presence-sync).",
		ConstLabels: labels,
	})
	reg.MustRegister(presenceKeys)

	presenceRouteTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        "signal_server_presence_route_total",
			Help:        "Cross-shard forward attempts by route: direct (presence-known owner) vs broadcast (fallback).",
			ConstLabels: labels,
		},
		[]string{"route"},
	)
	reg.MustRegister(presenceRouteTotal)

	return &metrics{
		relayTotal:           relayTotal,
		relayShedTotal:       relayShedTotal,
		deliverDuration:      deliverDuration,
		messageBytes:         messageBytes,
		forwardSentTotal:     forwardSentTotal,
		forwardReceivedTotal: forwardReceivedTotal,
		forwardDuration:      forwardDuration,
		presencePushTotal:    presencePushTotal,
		presenceKeys:         presenceKeys,
		presenceRouteTotal:   presenceRouteTotal,
	}
}
