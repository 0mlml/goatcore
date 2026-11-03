import { useState, useEffect, useRef, useCallback } from "preact/hooks";
import { getResults, getLatestResult, getPeers } from "./api/client";
import { colors, styles } from "./styles/theme";
import type { RunResult, Peer } from "./types";
import { TestRunner } from "./components/TestRunner";
import { ResultsMatrix } from "./components/ResultsMatrix";
import { ServiceHealth } from "./components/ServiceHealth";
import { PeerTable } from "./components/PeerTable";
import { LatencyChart } from "./components/LatencyChart";
import { HistoryBar } from "./components/HistoryBar";

export function App() {
  const [latest, setLatest] = useState<RunResult | null>(null);
  const [selected, setSelected] = useState<RunResult | null>(null);
  const [history, setHistory] = useState<RunResult[]>([]);
  const [peers, setPeers] = useState<Peer[]>([]);
  const peerInterval = useRef<number | null>(null);

  const loadData = useCallback(async () => {
    try {
      const [lat, hist, p] = await Promise.all([
        getLatestResult(),
        getResults(30),
        getPeers(),
      ]);
      setLatest(lat);
      setHistory(hist || []);
      setPeers(p || []);
      if (!selected && lat) setSelected(lat);
    } catch {
      // API may not be available yet
    }
  }, []);

  useEffect(() => {
    loadData();
    peerInterval.current = window.setInterval(async () => {
      try {
        const p = await getPeers();
        setPeers(p || []);
      } catch {
        // ignore
      }
    }, 30_000);
    return () => {
      if (peerInterval.current) clearInterval(peerInterval.current);
    };
  }, [loadData]);

  function handleRunComplete() {
    loadData();
  }

  function handleSelectRun(run: RunResult) {
    setSelected(run);
  }

  const active = selected || latest;

  return (
    <div style={styles.container}>
      <header style={styles.header}>
        <div>
          <h1 style={styles.title}>goat Smoke Test</h1>
          <p style={styles.subtitle}>
            Infrastructure health dashboard
            {latest && (
              <span>
                {" \u2014 "}last run {new Date(latest.timestamp).toLocaleString()}
                {" ("}
                {latest.duration_s.toFixed(1)}s, {latest.mode}
                {")"}
              </span>
            )}
          </p>
        </div>
        <TestRunner onRunComplete={handleRunComplete} />
      </header>

      {/* Summary strip */}
      {active && (
        <div
          style={{
            display: "flex",
            gap: "16px",
            marginBottom: "24px",
            flexWrap: "wrap",
          }}
        >
          <SummaryPill label="Pass" value={active.pass} color={colors.pass} />
          <SummaryPill label="Fail" value={active.fail} color={colors.fail} />
          <SummaryPill label="Warn" value={active.warn} color={colors.warn} />
          <SummaryPill
            label="Total"
            value={active.checks?.length || 0}
            color={colors.textDim}
          />
        </div>
      )}

      {/* History */}
      <section style={styles.section}>
        <div style={styles.sectionTitle}>Run History</div>
        <HistoryBar
          runs={history}
          onSelect={handleSelectRun}
          selectedRunId={active?.run_id}
        />
      </section>

      {/* Results Matrix */}
      <section style={styles.section}>
        <div style={styles.sectionTitle}>
          Results
          {active && active.run_id !== latest?.run_id && (
            <span
              style={{
                fontSize: "11px",
                color: colors.textDim,
                fontWeight: 400,
                marginLeft: "8px",
                textTransform: "none" as const,
              }}
            >
              (viewing {active.run_id.substring(0, 8)})
            </span>
          )}
        </div>
        <ResultsMatrix checks={active?.checks || []} />
      </section>

      {/* Latency */}
      <section style={styles.section}>
        <div style={styles.sectionTitle}>Relay Latency</div>
        <LatencyChart checks={active?.checks || []} />
      </section>

      {/* Service Health */}
      <section style={styles.section}>
        <div style={styles.sectionTitle}>Service Health</div>
        <ServiceHealth />
      </section>

      {/* Peer Table */}
      <section style={styles.section}>
        <div style={styles.sectionTitle}>Mesh Peers</div>
        <PeerTable peers={peers} />
      </section>
    </div>
  );
}

function SummaryPill({
  label,
  value,
  color,
}: {
  label: string;
  value: number;
  color: string;
}) {
  return (
    <div
      style={{
        background: colors.bgCard,
        border: `1px solid ${colors.border}`,
        borderRadius: "6px",
        padding: "10px 20px",
        textAlign: "center" as const,
        minWidth: "80px",
      }}
    >
      <div
        style={{
          fontSize: "24px",
          fontWeight: 700,
          color,
          fontFamily: "monospace",
        }}
      >
        {value}
      </div>
      <div
        style={{
          fontSize: "11px",
          color: colors.textDim,
          textTransform: "uppercase" as const,
          letterSpacing: "0.5px",
        }}
      >
        {label}
      </div>
    </div>
  );
}
