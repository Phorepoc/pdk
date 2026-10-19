// Wire types of the PhoneLab API (server/internal/{api,core,store,device}).
export type ParamType = 'string' | 'uint32' | 'int' | 'bool' | 'blob' | 'file';

export interface OpParam {
  name: string;
  type: ParamType | string;
  description: string;
  required: boolean;
}

export interface OpSpec {
  name: string;
  description: string;
  capability: string;
  params: OpParam[] | null;
  reboots: boolean;
}

export interface MonitorData {
  'mon.cpu'?: { busy?: number[] };
  'mon.ram'?: { free_kb?: number };
  'mon.sdio'?: { calls?: number; sectors?: number; ms?: number };
  [k: string]: unknown;
}

export interface Health {
  device: string;
  driver: string;
  transport: string;
  state: 'connected' | 'disconnected' | string;
  port?: string;
  connected_at?: string;
  last_byte?: string;
  last_byte_age_s: number;
  reconnects: number;
  last_error?: string;
  boot: number;
  boot_started?: string;
  slot?: string;
  busy_op?: string;
  monitor?: MonitorData;
  lines: number;
}

export interface HealthReport {
  ok: boolean;
  version: string;
  uptime_s: number;
  bus_dropped: number;
  devices: Health[];
}

export interface DeviceInfo {
  id: string;
  driver: string;
  health: Health;
  ops: OpSpec[];
}

export interface Line {
  id: number;
  device: string;
  boot: number;
  ts: number;
  text: string;
}

export interface DeviceEvent {
  id: number;
  device: string;
  boot: number;
  ts: number;
  kind: string;
  data: Record<string, unknown> | null;
}

export interface Boot {
  id: number;
  device: string;
  started: number | string;
  slot: string;
  outcome: string;
  pinned: boolean;
  lines: number;
  faults: number;
}

export interface OpRecord {
  id: number;
  device: string;
  ts: number;
  caller: string;
  op: string;
  args: unknown;
  result: unknown;
  error: string;
  ms: number;
}

export interface Blob {
  sha: string;
  kind: string;
  name: string;
  size: number;
  ts: number;
  device: string;
  boot: number;
  meta: Record<string, unknown> | null;
  pinned: boolean;
}

export interface Progress {
  op: string;
  stage: string;
  done: number;
  total: number;
}

export type StreamMsg =
  | { type: 'hello'; device: string; data: HealthReport }
  | { type: 'line'; device: string; data: Line }
  | { type: 'event'; device: string; data: DeviceEvent }
  | { type: 'state'; device: string; data: Health }
  | { type: 'progress'; device: string; data: Progress }
  | { type: 'op'; device: string; data: OpRecord };

export interface LineQuery { boot?: number | undefined; after?: number | undefined; before?: number | undefined; grep?: string | undefined; limit?: number | undefined }
export interface EventQuery { boot?: number; kind?: string; before?: number; limit?: number }

/** One entry of GET /api/devices/{id}/fs?path= (device op fs_list). */
export interface FsEntry { name: string; type: 'd' | 'f'; size: number; mtime?: number }
