import { useState } from "preact/hooks";
import type { Peer } from "../types";
import { colors, styles } from "../styles/theme";

interface Props {
  peers: Peer[];
}

type SortKey = "name" | "ip" | "connected" | "last_seen" | "os";

function formatTime(iso: string): string {
  if (!iso) return "\u2014";
  const d = new Date(iso);
  const now = new Date();
  const diffMs = now.getTime() - d.getTime();
  if (diffMs < 60_000) return "just now";
  if (diffMs < 3600_000) return `${Math.floor(diffMs / 60_000)}m ago`;
  if (diffMs < 86400_000) return `${Math.floor(diffMs / 3600_000)}h ago`;
  return d.toLocaleDateString();
}

export function PeerTable({ peers }: Props) {
  const [sortKey, setSortKey] = useState<SortKey>("name");
  const [sortAsc, setSortAsc] = useState(true);

  function handleSort(key: SortKey) {
    if (sortKey === key) {
      setSortAsc(!sortAsc);
    } else {
      setSortKey(key);
      setSortAsc(true);
    }
  }

  const sorted = [...peers].sort((a, b) => {
    let cmp = 0;
    switch (sortKey) {
      case "name":
        cmp = a.name.localeCompare(b.name);
        break;
      case "ip":
        cmp = a.ip.localeCompare(b.ip);
        break;
      case "connected":
        cmp = (a.connected ? 1 : 0) - (b.connected ? 1 : 0);
        break;
      case "last_seen":
        cmp =
          new Date(a.last_seen).getTime() - new Date(b.last_seen).getTime();
        break;
      case "os":
        cmp = a.os.localeCompare(b.os);
        break;
    }
    return sortAsc ? cmp : -cmp;
  });

  const connectedCount = peers.filter((p) => p.connected).length;

  const arrow = (key: SortKey) =>
    sortKey === key ? (sortAsc ? " \u25B2" : " \u25BC") : "";

  return (
    <div style={styles.card}>
      <div
        style={{
          marginBottom: "12px",
          fontSize: "13px",
          color: colors.textDim,
        }}
      >
        <span style={{ color: colors.pass, fontWeight: 600 }}>
          {connectedCount}
        </span>{" "}
        connected /{" "}
        <span style={{ fontWeight: 600 }}>{peers.length}</span> total
      </div>

      <table style={styles.table}>
        <thead>
          <tr>
            <th style={styles.th} onClick={() => handleSort("name")}>
              Name{arrow("name")}
            </th>
            <th style={styles.th} onClick={() => handleSort("ip")}>
              IP{arrow("ip")}
            </th>
            <th style={styles.th} onClick={() => handleSort("connected")}>
              Status{arrow("connected")}
            </th>
            <th style={styles.th} onClick={() => handleSort("last_seen")}>
              Last Seen{arrow("last_seen")}
            </th>
            <th style={styles.th} onClick={() => handleSort("os")}>
              OS{arrow("os")}
            </th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((p) => (
            <tr
              key={p.id}
              style={{
                opacity: p.connected ? 1 : 0.5,
              }}
            >
              <td style={{ ...styles.td, fontWeight: 500 }}>{p.name}</td>
              <td
                style={{
                  ...styles.td,
                  fontFamily: "monospace",
                  fontSize: "12px",
                }}
              >
                {p.ip}
              </td>
              <td style={styles.td}>
                <span
                  style={{
                    ...styles.badge,
                    background: p.connected ? colors.pass : colors.fail,
                    color: "#fff",
                  }}
                >
                  {p.connected ? "Connected" : "Offline"}
                </span>
              </td>
              <td
                style={{
                  ...styles.td,
                  fontSize: "12px",
                  color: colors.textDim,
                }}
              >
                {formatTime(p.last_seen)}
              </td>
              <td
                style={{
                  ...styles.td,
                  fontSize: "12px",
                  color: colors.textDim,
                }}
              >
                {p.os}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      {peers.length === 0 && (
        <div
          style={{
            padding: "20px",
            textAlign: "center",
            color: colors.textDim,
            fontSize: "13px",
          }}
        >
          No peers found.
        </div>
      )}
    </div>
  );
}
