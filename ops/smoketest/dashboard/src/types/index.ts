export type Status = "PASS" | "FAIL" | "WARN" | "SKIP";
export type Category =
  | "dns"
  | "relay"
  | "stun"
  | "tls"
  | "mesh"
  | "service"
  | "latency"
  | "load";

export interface Result {
  category: Category;
  name: string;
  status: Status;
  detail: string;
  latency_ms?: number;
  timestamp: string;
  metadata?: Record<string, any>;
}

export interface RunResult {
  run_id: string;
  timestamp: string;
  mode: string;
  duration_s: number;
  pass: number;
  fail: number;
  warn: number;
  checks: Result[];
}

export interface Peer {
  id: string;
  name: string;
  ip: string;
  connected: boolean;
  last_seen: string;
  os: string;
  version: string;
}

export interface ServiceEntry {
  name: string;
  description: string;
  url?: string;
  category: string;
}

export interface RunStatus {
  running: boolean;
  run_id: string;
  elapsed_s: number;
}

export interface TriggerResponse {
  status: string;
  run_id: string;
}
