import { useState, useEffect } from "preact/hooks";
import { getCatalogue, getServices } from "../api/client";
import { colors, styles, statusColor } from "../styles/theme";
import type { ServiceEntry, Result } from "../types";

export function ServiceHealth() {
  const [catalogue, setCatalogue] = useState<ServiceEntry[]>([]);
  const [probes, setProbes] = useState<Result[]>([]);
  const [error, setError] = useState(false);
  const [selected, setSelected] = useState<ServiceEntry | null>(null);

  useEffect(() => {
    Promise.all([getCatalogue(), getServices()])
      .then(([cat, svc]) => {
        setCatalogue(cat);
        setProbes(svc.checks || []);
      })
      .catch(() => setError(true));
  }, []);

  if (error) {
    return (
      <div style={{ ...styles.card, color: colors.textDim, fontSize: "13px" }}>
        Unable to load service health data.
      </div>
    );
  }

  if (!catalogue.length) {
    return (
      <div style={{ ...styles.card, color: colors.textDim, fontSize: "13px" }}>
        No services in catalogue.
      </div>
    );
  }

  // Probe results use "svc-" prefix on names (e.g. "svc-netbird-management").
  // Build map keyed by the catalogue name (without prefix).
  const probeMap = new Map<string, Result>();
  for (const p of probes) {
    const key = p.name.startsWith("svc-") ? p.name.slice(4) : p.name;
    probeMap.set(key, p);
  }

  return (
    <>
      <div style={styles.grid}>
        {catalogue.map((svc) => {
          const probe = probeMap.get(svc.name);
          const status = probe?.status || "SKIP";
          const dotColor = statusColor[status] || colors.skip;

          return (
            <div
              key={svc.name}
              onClick={() => setSelected(svc)}
              style={{
                ...styles.card,
                display: "flex",
                flexDirection: "column",
                gap: "8px",
                cursor: "pointer",
                transition: "border-color 0.15s",
                borderColor: selected?.name === svc.name ? colors.sand : colors.border,
              }}
            >
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "space-between",
                }}
              >
                <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                  <span
                    style={{
                      width: "10px",
                      height: "10px",
                      borderRadius: "50%",
                      background: dotColor,
                      display: "inline-block",
                      boxShadow: `0 0 6px ${dotColor}`,
                    }}
                  />
                  <span style={{ fontWeight: 600, fontSize: "14px" }}>
                    {svc.name}
                  </span>
                </div>
                <span
                  style={{
                    ...styles.badge,
                    background: colors.bgLight,
                    color: colors.sandDim,
                    fontSize: "10px",
                  }}
                >
                  {svc.category}
                </span>
              </div>

              <div
                style={{
                  fontSize: "12px",
                  color: colors.textDim,
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                {svc.description}
              </div>

              {probe && (
                <div style={{ fontSize: "12px", color: colors.textDim }}>
                  {probe.latency_ms != null && (
                    <span style={{ marginRight: "12px" }}>
                      {probe.latency_ms.toFixed(1)}ms
                    </span>
                  )}
                  <span>{probe.detail}</span>
                </div>
              )}
            </div>
          );
        })}
      </div>

      {selected && (
        <ServiceDetailOverlay
          svc={selected}
          probe={probeMap.get(selected.name) || null}
          onClose={() => setSelected(null)}
        />
      )}
    </>
  );
}

const phaseColors: Record<string, string> = {
  dns_ms: "#58a6ff",
  tcp_ms: "#3fb950",
  tls_ms: "#d29922",
  ttfb_ms: "#f85149",
};

const phaseLabels: Record<string, string> = {
  dns_ms: "DNS",
  tcp_ms: "TCP",
  tls_ms: "TLS",
  ttfb_ms: "TTFB",
};

function PhaseBreakdown({ metadata }: { metadata: Record<string, any> }) {
  const phases = ["dns_ms", "tcp_ms", "tls_ms", "ttfb_ms"];
  const present = phases.filter((p) => metadata[p] != null);
  if (!present.length) return null;

  const total = metadata.total_ms || present.reduce((s, p) => s + (metadata[p] || 0), 0);
  const maxBar = 200; // px

  return (
    <div style={{ marginBottom: "12px" }}>
      <div style={{ fontSize: "11px", color: colors.sandDim, marginBottom: "6px", textTransform: "uppercase", letterSpacing: "0.5px" }}>
        Latency Breakdown
      </div>
      {present.map((phase) => {
        const ms = metadata[phase] as number;
        const pct = total > 0 ? (ms / total) * 100 : 0;
        const barWidth = total > 0 ? Math.max(2, (ms / total) * maxBar) : 2;
        return (
          <div key={phase} style={{ display: "flex", alignItems: "center", gap: "8px", marginBottom: "4px" }}>
            <span style={{ width: "36px", fontSize: "11px", color: phaseColors[phase], fontWeight: 600, textAlign: "right" }}>
              {phaseLabels[phase]}
            </span>
            <div style={{ flex: 1, height: "8px", background: colors.bg, borderRadius: "4px", overflow: "hidden" }}>
              <div
                style={{
                  width: `${barWidth}px`,
                  maxWidth: "100%",
                  height: "100%",
                  background: phaseColors[phase],
                  borderRadius: "4px",
                  transition: "width 0.3s",
                }}
              />
            </div>
            <span style={{ width: "60px", fontSize: "11px", fontFamily: "monospace", color: colors.textDim }}>
              {ms.toFixed(1)}ms
            </span>
            <span style={{ width: "36px", fontSize: "10px", color: colors.textDim, textAlign: "right" }}>
              {pct.toFixed(0)}%
            </span>
          </div>
        );
      })}
    </div>
  );
}

function ServiceDetailOverlay({
  svc,
  probe,
  onClose,
}: {
  svc: ServiceEntry;
  probe: Result | null;
  onClose: () => void;
}) {
  const status = probe?.status || "SKIP";
  const dotColor = statusColor[status] || colors.skip;

  return (
    <div
      onClick={onClose}
      style={{
        position: "fixed",
        inset: 0,
        background: "rgba(0,0,0,0.6)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        zIndex: 1000,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          ...styles.card,
          background: colors.bgLight,
          maxWidth: "520px",
          width: "90%",
          maxHeight: "80vh",
          overflow: "auto",
          position: "relative",
        }}
      >
        <button
          onClick={onClose}
          style={{
            position: "absolute",
            top: "12px",
            right: "12px",
            background: "none",
            border: "none",
            color: colors.textDim,
            fontSize: "18px",
            cursor: "pointer",
            padding: "4px 8px",
          }}
        >
          x
        </button>

        <div style={{ display: "flex", alignItems: "center", gap: "10px", marginBottom: "16px" }}>
          <span
            style={{
              width: "12px",
              height: "12px",
              borderRadius: "50%",
              background: dotColor,
              display: "inline-block",
              boxShadow: `0 0 8px ${dotColor}`,
            }}
          />
          <h3 style={{ fontSize: "18px", fontWeight: 700, color: colors.sandLight, margin: 0 }}>
            {svc.name}
          </h3>
          <span
            style={{
              ...styles.badge,
              background: colors.bgCard,
              color: colors.sandDim,
              fontSize: "10px",
              marginLeft: "auto",
            }}
          >
            {svc.category}
          </span>
        </div>

        <div style={{ fontSize: "13px", color: colors.text, marginBottom: "12px" }}>
          {svc.description}
        </div>

        {svc.url && (
          <div style={{ fontSize: "12px", color: colors.textDim, marginBottom: "12px" }}>
            <span style={{ color: colors.sandDim }}>URL:</span>{" "}
            <span style={{ fontFamily: "monospace" }}>{svc.url}</span>
          </div>
        )}

        {probe && (
          <div
            style={{
              background: colors.bgCard,
              borderRadius: "4px",
              padding: "12px",
              fontSize: "13px",
            }}
          >
            <div style={{ marginBottom: "8px" }}>
              <span style={{ color: colors.sandDim }}>Status:</span>{" "}
              <span style={{ color: dotColor, fontWeight: 600 }}>{probe.status}</span>
            </div>

            {probe.latency_ms != null && (
              <div style={{ marginBottom: "8px" }}>
                <span style={{ color: colors.sandDim }}>Total Latency:</span>{" "}
                {probe.latency_ms.toFixed(1)}ms
              </div>
            )}

            {probe.metadata && <PhaseBreakdown metadata={probe.metadata} />}

            <div style={{ marginBottom: "8px" }}>
              <span style={{ color: colors.sandDim }}>Detail:</span>{" "}
              <span style={{ fontFamily: "monospace", fontSize: "12px" }}>{probe.detail}</span>
            </div>

            <div>
              <span style={{ color: colors.sandDim }}>Checked:</span>{" "}
              <span style={{ fontSize: "12px" }}>{new Date(probe.timestamp).toLocaleString()}</span>
            </div>
          </div>
        )}

        {!probe && (
          <div style={{ fontSize: "13px", color: colors.textDim, fontStyle: "italic" }}>
            No probe results yet — run a smoke test to check this service.
          </div>
        )}
      </div>
    </div>
  );
}
