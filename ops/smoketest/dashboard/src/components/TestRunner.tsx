import { useState, useEffect, useRef } from "preact/hooks";
import { triggerRun, getRunStatus } from "../api/client";
import { colors, styles } from "../styles/theme";
import type { RunStatus } from "../types";

interface Props {
  onRunComplete: () => void;
}

export function TestRunner({ onRunComplete }: Props) {
  const [mode, setMode] = useState<"quick" | "full">("quick");
  const [status, setStatus] = useState<RunStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const pollRef = useRef<number | null>(null);

  const running = status?.running ?? false;

  function startPolling() {
    if (pollRef.current) return;
    pollRef.current = window.setInterval(async () => {
      try {
        const s = await getRunStatus();
        setStatus(s);
        if (!s.running) {
          stopPolling();
          onRunComplete();
        }
      } catch {
        stopPolling();
      }
    }, 1000);
  }

  function stopPolling() {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }

  useEffect(() => {
    getRunStatus()
      .then((s) => {
        setStatus(s);
        if (s.running) startPolling();
      })
      .catch(() => {});
    return stopPolling;
  }, []);

  async function handleRun() {
    setError(null);
    try {
      await triggerRun(mode);
      const s = await getRunStatus();
      setStatus(s);
      startPolling();
    } catch (e: any) {
      if (e.message?.includes("409")) {
        setError("A test run is already in progress");
      } else {
        setError(e.message || "Failed to start run");
      }
    }
  }

  const toggleStyle: Record<string, string | number> = {
    display: "inline-flex",
    borderRadius: "4px",
    overflow: "hidden",
    border: `1px solid ${colors.border}`,
    marginRight: "12px",
    fontSize: "12px",
  };

  const optStyle = (active: boolean): Record<string, string | number> => ({
    padding: "6px 14px",
    cursor: "pointer",
    background: active ? colors.sand : "transparent",
    color: active ? colors.bg : colors.textDim,
    fontWeight: active ? 600 : 400,
    border: "none",
    fontFamily: "system-ui, -apple-system, sans-serif",
    fontSize: "12px",
  });

  return (
    <div style={{ display: "flex", alignItems: "center", gap: "12px", flexWrap: "wrap" }}>
      <div style={toggleStyle}>
        <button
          style={optStyle(mode === "quick")}
          onClick={() => setMode("quick")}
          disabled={running}
        >
          Quick
        </button>
        <button
          style={optStyle(mode === "full")}
          onClick={() => setMode("full")}
          disabled={running}
        >
          Full
        </button>
      </div>

      <button
        style={{
          ...styles.button,
          ...(running ? styles.buttonDisabled : {}),
        }}
        onClick={handleRun}
        disabled={running}
      >
        {running ? "Running..." : "Run Smoke Test"}
      </button>

      {running && status && (
        <span style={{ fontSize: "13px", color: colors.textDim }}>
          {status.elapsed_s.toFixed(1)}s elapsed
        </span>
      )}

      {error && (
        <span style={{ fontSize: "13px", color: colors.fail }}>{error}</span>
      )}
    </div>
  );
}
