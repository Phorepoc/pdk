// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Typed REST client. All URLs are relative so the SPA works from any host.
import type {
  Blob, Boot, FsEntry, DeviceEvent, DeviceInfo, EventQuery, HealthReport, Line, LineQuery, OpRecord,
} from './types';

export interface FrameResponse {
  status: number;
  error?: string;
  blob?: globalThis.Blob;
  width?: number;
  height?: number;
  seconds?: number;
  wire?: number; // bytes over the USB link for this frame (live view: changed tiles only)
  tiles?: number;
  sha?: string;
}

export class ApiError extends Error {
  status: number;
  body: unknown;
  /** op result carried by a 422 (the device ran the op and it failed) */
  result?: unknown;
  constructor(status: number, message: string, body?: unknown, result?: unknown) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.body = body;
    this.result = result;
  }
  get unauthorized(): boolean { return this.status === 401; }
}

const TOKEN_KEY = 'phonelab.token';
let token = '';
let unauthorizedHandler: ((e: ApiError) => void) | null = null;

export function loadStoredToken(): string {
  try { return localStorage.getItem(TOKEN_KEY) ?? ''; } catch { return ''; }
}
export function setToken(t: string): void {
  token = t.trim();
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token); else localStorage.removeItem(TOKEN_KEY);
  } catch { /* storage blocked: token lives for this tab only */ }
}
export function getToken(): string { return token; }
export function onUnauthorized(fn: ((e: ApiError) => void) | null): void { unauthorizedHandler = fn; }

function headers(extra?: Record<string, string>): Record<string, string> {
  const h: Record<string, string> = { 'X-Phonelab-Caller': 'web', ...extra };
  if (token) h['Authorization'] = `Bearer ${token}`;
  return h;
}

async function parse(res: Response): Promise<unknown> {
  const text = await res.text();
  if (!text) return null;
  try { return JSON.parse(text); } catch { return text; }
}

async function request<T>(path: string, init: RequestInit = {}, okStatuses: number[] = []): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, { ...init, headers: headers(init.headers as Record<string, string>) });
  } catch (e) {
    throw new ApiError(0, `network error: ${(e as Error).message}`);
  }
  const body = await parse(res);
  if (res.ok || okStatuses.includes(res.status)) return body as T;
  const b = body as { error?: string; result?: unknown } | string | null;
  const msg = (typeof b === 'object' && b?.error) || (typeof b === 'string' && b) || `HTTP ${res.status}`;
  const err = new ApiError(res.status, String(msg), body, typeof b === 'object' && b ? b.result : undefined);
  if (res.status === 401) unauthorizedHandler?.(err);
  throw err;
}

function qs(o: object): string {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(o)) if (v !== undefined && v !== null && v !== '' && v !== 0) p.set(k, k === 'boot' && v === -1 ? 'current' : String(v));
  const s = p.toString();
  return s ? `?${s}` : '';
}

/** A request with upload progress (XHR: fetch cannot report upload progress). */
function xhr<T>(method: string, url: string, body: XMLHttpRequestBodyInit, onProgress?: (done: number, total: number) => void): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const x = new XMLHttpRequest();
    x.open(method, url);
    for (const [k, v] of Object.entries(headers())) x.setRequestHeader(k, v);
    x.upload.onprogress = (e) => { if (e.lengthComputable) onProgress?.(e.loaded, e.total); };
    x.onerror = () => reject(new ApiError(0, 'network error during upload'));
    x.onload = () => {
      let b: unknown = null;
      try { b = JSON.parse(x.responseText); } catch { b = x.responseText; }
      if (x.status >= 200 && x.status < 300) { resolve(b as T); return; }
      const err = new ApiError(x.status, (b as { error?: string } | null)?.error ?? `HTTP ${x.status}`, b);
      if (x.status === 401) unauthorizedHandler?.(err);
      reject(err);
    };
    x.send(body);
  });
}

const dev = (id: string) => `/api/devices/${encodeURIComponent(id)}`;

export const api = {
  /** /api/health answers 503 when a device is down, with the same body. */
  health: () => request<HealthReport>('/api/health', {}, [503]),
  devices: () => request<DeviceInfo[]>('/api/devices'),
  /** Runs an op. Resolves with `result`; a 422 rejects with ApiError (.result = partial result). */
  op: async (id: string, op: string, args: Record<string, unknown> = {}, caller = ''): Promise<unknown> => {
    const r = await request<{ result: unknown }>(`${dev(id)}/ops/${encodeURIComponent(op)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...(caller ? { 'X-Phonelab-Caller': caller } : {}) },
      body: JSON.stringify(args),
    });
    return r?.result;
  },
  lines: (id: string, q: LineQuery = {}) => request<Line[]>(`${dev(id)}/lines${qs(q)}`),
  events: (id: string, q: EventQuery = {}) => request<DeviceEvent[]>(`${dev(id)}/events${qs(q)}`),
  boots: (id: string, before = 0, limit = 50) => request<Boot[]>(`${dev(id)}/boots${qs({ before, limit })}`),
  history: (id: string, before = 0, limit = 50) => request<OpRecord[]>(`${dev(id)}/history${qs({ before, limit })}`),
  /** Phone file service (IDE Project panel). 501 = the phone side is not implemented yet (see docs). */
  fsList: (id: string, path: string) => request<{ path: string; entries: FsEntry[] }>(`${dev(id)}/fs${qs({ path })}`),
  fsUrl: (id: string, path: string): string => {
    const p = new URLSearchParams({ path });
    if (token) p.set('token', token);
    return `${dev(id)}/fs/content?${p.toString()}`;
  },
  fsPut: (id: string, path: string, file: File, onProgress?: (done: number, total: number) => void) =>
    xhr<{ path: string; written: number }>('PUT', `${dev(id)}/fs/content${qs({ path })}`, file, onProgress),
  fsDelete: (id: string, path: string) =>
    request<{ deleted: string }>(`${dev(id)}/fs/content${qs({ path })}`, { method: 'DELETE' }),
  /** The phone's phetty ttys. running = false: phetty is not reachable (error says why). */
  ttys: (id: string, start = false) =>
    request<{ implemented: boolean; running?: boolean; error?: string; ttys: { id: number; cols: number; rows: number; shell: number; title: string }[] }>(
      `${dev(id)}/ttys${qs({ start: start ? 1 : 0 })}`),
  blobs: (kind = '', limit = 0) => request<Blob[]>(`/api/blobs${qs({ kind, limit })}`),
  pin: (what: 'boot' | 'blob', id: string | number, on: boolean) =>
    request<{ ok: boolean }>('/api/pin', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ what, id: String(id), on }),
    }),
  /** URL for an <img src> / download link. img tags cannot send the bearer header, so with a token
   *  set this appends ?token= (the server accepts it on any /api path). */
  blobUrl: (sha: string, download = false): string => {
    const p = new URLSearchParams();
    if (download) p.set('download', '1');
    if (token) p.set('token', token);
    const s = p.toString();
    return `/api/blobs/${sha}${s ? `?${s}` : ''}`;
  },
  /** One live frame (PNG). 409 = link busy, 502 = capture error; neither throws. */
  frame: async (id: string, which: number, store = false, signal?: AbortSignal): Promise<FrameResponse> => {
    const q = qs({ store: store ? 1 : 0 });
    const url = `${dev(id)}/frame?which=${which}${q ? '&' + q.slice(1) : ''}`;
    let res: Response;
    try { res = await fetch(url, { headers: headers(), ...(signal ? { signal } : {}) }); }
    catch (e) { return { status: 0, error: (e as Error).message }; }
    if (res.status === 401) unauthorizedHandler?.(new ApiError(401, 'unauthorized'));
    if (!res.ok) {
      const b = await parse(res);
      return { status: res.status, error: (b as { error?: string } | null)?.error ?? `HTTP ${res.status}` };
    }
    const blob = await res.blob();
    const n = (h: string) => Number(res.headers.get(h) ?? 0);
    return { status: 200, blob, width: n('X-Frame-Width'), height: n('X-Frame-Height'), seconds: n('X-Frame-Seconds'), wire: n('X-Frame-Wire'), tiles: n('X-Frame-Tiles'), sha: res.headers.get('X-Blob') ?? '' };
  },
  /** Multipart upload with progress (XHR: fetch cannot report upload progress). */
  uploadBlob: (file: File, kind = 'image', device = '', onProgress?: (done: number, total: number) => void) => {
    const fd = new FormData();
    fd.append('file', file, file.name);
    return xhr<Blob>('POST', `/api/blobs${qs({ kind, device })}`, fd, onProgress);
  },
};
