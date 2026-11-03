import type { JSX } from "preact";

type CSSProps = JSX.CSSProperties;

export const colors = {
  bg: "#1a1a2e",
  bgLight: "#222244",
  bgCard: "#252547",
  bgHover: "#2a2a5a",
  sand: "#d4a574",
  sandLight: "#e8c9a0",
  sandDim: "#a07d52",
  pass: "#2ecc71",
  fail: "#e74c3c",
  warn: "#f1c40f",
  skip: "#7f8c8d",
  text: "#e0e0e0",
  textDim: "#8888aa",
  border: "#333366",
} as const;

export const statusColor: Record<string, string> = {
  PASS: colors.pass,
  FAIL: colors.fail,
  WARN: colors.warn,
  SKIP: colors.skip,
};

export const styles = {
  container: {
    maxWidth: "1200px",
    margin: "0 auto",
    padding: "20px",
    fontFamily: "system-ui, -apple-system, sans-serif",
    color: colors.text,
  } satisfies CSSProps,

  header: {
    display: "flex",
    justifyContent: "space-between",
    alignItems: "center",
    marginBottom: "24px",
    paddingBottom: "16px",
    borderBottom: `1px solid ${colors.border}`,
  } satisfies CSSProps,

  title: {
    fontSize: "24px",
    fontWeight: 700,
    color: colors.sand,
    margin: 0,
  } satisfies CSSProps,

  subtitle: {
    fontSize: "13px",
    color: colors.textDim,
    margin: "4px 0 0 0",
  } satisfies CSSProps,

  section: {
    marginBottom: "28px",
  } satisfies CSSProps,

  sectionTitle: {
    fontSize: "16px",
    fontWeight: 600,
    color: colors.sandLight,
    marginBottom: "12px",
    textTransform: "uppercase" as const,
    letterSpacing: "0.5px",
  } satisfies CSSProps,

  card: {
    background: colors.bgCard,
    border: `1px solid ${colors.border}`,
    borderRadius: "6px",
    padding: "16px",
  } satisfies CSSProps,

  table: {
    width: "100%",
    borderCollapse: "collapse" as const,
    fontSize: "13px",
  } satisfies CSSProps,

  th: {
    textAlign: "left" as const,
    padding: "8px 12px",
    borderBottom: `2px solid ${colors.border}`,
    color: colors.sandDim,
    fontWeight: 600,
    fontSize: "11px",
    textTransform: "uppercase" as const,
    letterSpacing: "0.5px",
    cursor: "pointer",
    userSelect: "none" as const,
  } satisfies CSSProps,

  td: {
    padding: "8px 12px",
    borderBottom: `1px solid ${colors.border}`,
  } satisfies CSSProps,

  badge: {
    display: "inline-block",
    padding: "2px 8px",
    borderRadius: "3px",
    fontSize: "11px",
    fontWeight: 600,
    textTransform: "uppercase" as const,
  } satisfies CSSProps,

  button: {
    background: colors.sand,
    color: colors.bg,
    border: "none",
    borderRadius: "4px",
    padding: "8px 16px",
    fontSize: "13px",
    fontWeight: 600,
    cursor: "pointer",
    fontFamily: "system-ui, -apple-system, sans-serif",
  } satisfies CSSProps,

  buttonDisabled: {
    background: colors.sandDim,
    cursor: "not-allowed",
    opacity: 0.6,
  } satisfies CSSProps,

  grid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))",
    gap: "12px",
  } satisfies CSSProps,
};

export const globalStyles = `
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    background: ${colors.bg};
    color: ${colors.text};
    font-family: system-ui, -apple-system, sans-serif;
    font-size: 14px;
    line-height: 1.5;
  }
  a { color: ${colors.sand}; text-decoration: none; }
  a:hover { color: ${colors.sandLight}; }
  ::-webkit-scrollbar { width: 8px; }
  ::-webkit-scrollbar-track { background: ${colors.bg}; }
  ::-webkit-scrollbar-thumb { background: ${colors.border}; border-radius: 4px; }
`;
