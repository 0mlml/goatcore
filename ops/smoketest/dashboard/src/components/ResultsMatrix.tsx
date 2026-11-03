import type { Result, Category } from "../types";
import { colors, styles, statusColor } from "../styles/theme";

interface Props {
  checks: Result[];
}

const CATEGORY_ORDER: Category[] = [
  "dns",
  "tls",
  "relay",
  "stun",
  "mesh",
  "service",
  "latency",
  "load",
];

const categoryLabel: Record<Category, string> = {
  dns: "DNS",
  tls: "TLS",
  relay: "Relay",
  stun: "STUN",
  mesh: "Mesh",
  service: "Service",
  latency: "Latency",
  load: "Load",
};

function displayName(name: string): string {
  return name.startsWith("svc-") ? name.slice(4) : name;
}

export function ResultsMatrix({ checks }: Props) {
  if (!checks.length) {
    return (
      <div style={{ ...styles.card, color: colors.textDim, fontSize: "13px" }}>
        No results yet. Run a smoke test to see results.
      </div>
    );
  }

  const grouped = new Map<Category, Result[]>();
  for (const c of checks) {
    const list = grouped.get(c.category) || [];
    list.push(c);
    grouped.set(c.category, list);
  }

  const sortedCategories = CATEGORY_ORDER.filter((c) => grouped.has(c));

  return (
    <div style={styles.card}>
      <table style={styles.table}>
        <thead>
          <tr>
            <th style={styles.th}>Check</th>
            <th style={{ ...styles.th, width: "70px" }}>Status</th>
            <th style={styles.th}>Detail</th>
            <th style={{ ...styles.th, width: "80px", textAlign: "right" as const }}>
              Latency
            </th>
          </tr>
        </thead>
        <tbody>
          {sortedCategories.map((cat) => {
            const items = grouped.get(cat)!;
            return (
              <>
                <tr key={`cat-${cat}`}>
                  <td
                    colSpan={4}
                    style={{
                      ...styles.td,
                      background: colors.bgLight,
                      fontWeight: 600,
                      fontSize: "12px",
                      color: colors.sand,
                      textTransform: "uppercase" as const,
                      letterSpacing: "0.5px",
                      borderBottom: `1px solid ${colors.border}`,
                    }}
                  >
                    {categoryLabel[cat]}
                  </td>
                </tr>
                {items.map((r, i) => (
                  <tr
                    key={`${cat}-${i}`}
                    style={{
                      borderLeft: `3px solid ${statusColor[r.status] || colors.skip}`,
                    }}
                  >
                    <td style={styles.td}>{displayName(r.name)}</td>
                    <td style={styles.td}>
                      <span
                        style={{
                          ...styles.badge,
                          background: statusColor[r.status] || colors.skip,
                          color: r.status === "WARN" ? colors.bg : "#fff",
                        }}
                      >
                        {r.status}
                      </span>
                    </td>
                    <td
                      style={{
                        ...styles.td,
                        color: colors.textDim,
                        fontSize: "12px",
                        maxWidth: "400px",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                      }}
                    >
                      {r.detail}
                    </td>
                    <td
                      style={{
                        ...styles.td,
                        textAlign: "right" as const,
                        fontFamily: "monospace",
                        fontSize: "12px",
                        color: colors.textDim,
                      }}
                    >
                      {r.latency_ms != null ? `${r.latency_ms.toFixed(1)}ms` : "\u2014"}
                    </td>
                  </tr>
                ))}
              </>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
