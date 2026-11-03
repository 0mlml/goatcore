import type { RunResult } from "../types";
import { colors } from "../styles/theme";

interface Props {
  runs: RunResult[];
  onSelect: (run: RunResult) => void;
  selectedRunId?: string;
}

function runColor(run: RunResult): string {
  if (run.fail > 0) return colors.fail;
  if (run.warn > 0) return colors.warn;
  return colors.pass;
}

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function HistoryBar({ runs, onSelect, selectedRunId }: Props) {
  if (!runs.length) {
    return (
      <div style={{ color: colors.textDim, fontSize: "13px" }}>
        No run history.
      </div>
    );
  }

  return (
    <div
      style={{
        display: "flex",
        gap: "4px",
        alignItems: "flex-end",
        flexWrap: "wrap",
      }}
    >
      {runs.map((run) => {
        const bg = runColor(run);
        const isSelected = run.run_id === selectedRunId;

        return (
          <div
            key={run.run_id}
            title={`${formatTimestamp(run.timestamp)} \u2014 ${run.pass}P ${run.fail}F ${run.warn}W (${run.duration_s.toFixed(1)}s)`}
            onClick={() => onSelect(run)}
            style={{
              width: "22px",
              height: "22px",
              borderRadius: "3px",
              background: bg,
              cursor: "pointer",
              opacity: isSelected ? 1 : 0.7,
              outline: isSelected
                ? `2px solid ${colors.sand}`
                : "none",
              outlineOffset: "1px",
              transition: "opacity 0.15s ease",
            }}
            onMouseEnter={(e) => {
              (e.currentTarget as HTMLElement).style.opacity = "1";
            }}
            onMouseLeave={(e) => {
              if (!isSelected) {
                (e.currentTarget as HTMLElement).style.opacity = "0.7";
              }
            }}
          />
        );
      })}
    </div>
  );
}
