import type { Result } from "../types";
import { colors, styles } from "../styles/theme";

interface Props {
  checks: Result[];
}

export function LatencyChart({ checks }: Props) {
  const latencyChecks = checks.filter(
    (c) => c.category === "latency" && c.latency_ms != null
  );

  if (!latencyChecks.length) {
    return (
      <div style={{ ...styles.card, color: colors.textDim, fontSize: "13px" }}>
        No latency data available.
      </div>
    );
  }

  const values = latencyChecks.map((c) => c.latency_ms!);
  const maxLatency = Math.max(...values, 1);
  const sorted = [...values].sort((a, b) => a - b);
  const avg = values.reduce((s, v) => s + v, 0) / values.length;
  const p50 = sorted[Math.floor(sorted.length * 0.5)];
  const p95 = sorted[Math.floor(sorted.length * 0.95)];

  function barColor(ms: number): string {
    if (ms < 50) return colors.pass;
    if (ms < 150) return colors.warn;
    return colors.fail;
  }

  return (
    <div style={styles.card}>
      <div
        style={{
          display: "flex",
          gap: "24px",
          marginBottom: "16px",
          fontSize: "13px",
        }}
      >
        <div>
          <span style={{ color: colors.textDim }}>Avg: </span>
          <span style={{ fontWeight: 600, fontFamily: "monospace" }}>
            {avg.toFixed(1)}ms
          </span>
        </div>
        <div>
          <span style={{ color: colors.textDim }}>P50: </span>
          <span style={{ fontWeight: 600, fontFamily: "monospace" }}>
            {p50.toFixed(1)}ms
          </span>
        </div>
        <div>
          <span style={{ color: colors.textDim }}>P95: </span>
          <span style={{ fontWeight: 600, fontFamily: "monospace" }}>
            {p95.toFixed(1)}ms
          </span>
        </div>
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: "6px" }}>
        {latencyChecks.map((c, i) => {
          const pct = Math.max((c.latency_ms! / maxLatency) * 100, 2);
          return (
            <div
              key={i}
              style={{
                display: "flex",
                alignItems: "center",
                gap: "12px",
              }}
            >
              <div
                style={{
                  width: "140px",
                  flexShrink: 0,
                  fontSize: "12px",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                {c.name}
              </div>
              <div
                style={{
                  flex: 1,
                  height: "20px",
                  background: colors.bgLight,
                  borderRadius: "3px",
                  overflow: "hidden",
                  position: "relative",
                }}
              >
                <div
                  style={{
                    width: `${pct}%`,
                    height: "100%",
                    background: barColor(c.latency_ms!),
                    borderRadius: "3px",
                    transition: "width 0.3s ease",
                  }}
                />
              </div>
              <div
                style={{
                  width: "70px",
                  textAlign: "right" as const,
                  fontSize: "12px",
                  fontFamily: "monospace",
                  color: colors.textDim,
                  flexShrink: 0,
                }}
              >
                {c.latency_ms!.toFixed(1)}ms
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
