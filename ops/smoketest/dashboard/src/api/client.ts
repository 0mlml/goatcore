import type {
  RunResult,
  RunStatus,
  TriggerResponse,
  Peer,
  ServiceEntry,
} from "../types";

const BASE = "";

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${url}`, init);
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`${res.status}: ${text}`);
  }
  return res.json();
}

export function triggerRun(
  mode: "quick" | "full"
): Promise<TriggerResponse> {
  return fetchJSON("/api/run", {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: `mode=${mode}`,
  });
}

export function getRunStatus(): Promise<RunStatus> {
  return fetchJSON("/api/run/status");
}

export function getResults(limit = 20): Promise<RunResult[]> {
  return fetchJSON(`/api/results?limit=${limit}`);
}

export function getLatestResult(): Promise<RunResult | null> {
  return fetchJSON<RunResult | null>("/api/results/latest");
}

export function getPeers(): Promise<Peer[]> {
  return fetchJSON("/api/peers");
}

export function getServices(): Promise<RunResult> {
  return fetchJSON("/api/services");
}

export function getCatalogue(): Promise<ServiceEntry[]> {
  return fetchJSON("/api/catalogue");
}
