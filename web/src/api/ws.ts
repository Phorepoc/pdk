// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// WebSocket stream with reconnect + exponential backoff. Dispatches parsed JSON messages by callback.
import { getToken } from './client';
import type { StreamMsg } from './types';

export interface SocketOptions {
  types?: string[];
  onMessage: (m: StreamMsg) => void;
  onOpen?: () => void;
  /** called on every close; `everOpened` is false when the handshake itself failed (e.g. 401) */
  onClose?: (everOpened: boolean) => void;
  minDelay?: number;
  maxDelay?: number;
}

export function streamUrl(types?: string[]): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const p = new URLSearchParams();
  if (types?.length) p.set('types', types.join(','));
  const t = getToken();
  if (t) p.set('token', t);
  const s = p.toString();
  return `${proto}//${location.host}/api/stream${s ? `?${s}` : ''}`;
}

export class LiveSocket {
  private ws: WebSocket | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private delay: number;
  private stopped = true;
  private opened = false;
  constructor(private o: SocketOptions) { this.delay = o.minDelay ?? 500; }

  get connected(): boolean { return this.ws?.readyState === 1; }

  start(): void {
    if (!this.stopped) return;
    this.stopped = false;
    this.connect();
  }

  stop(): void {
    this.stopped = true;
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    const ws = this.ws;
    this.ws = null;
    ws?.close();
  }

  /** Drop the current socket and reconnect right away (e.g. after the token changed). */
  restart(): void { this.stop(); this.delay = this.o.minDelay ?? 500; this.start(); }

  private connect(): void {
    if (this.stopped) return;
    this.opened = false;
    const ws = new WebSocket(streamUrl(this.o.types));
    this.ws = ws;
    ws.onopen = () => { this.opened = true; this.delay = this.o.minDelay ?? 500; this.o.onOpen?.(); };
    ws.onmessage = (ev: MessageEvent) => {
      try { this.o.onMessage(JSON.parse(String(ev.data)) as StreamMsg); } catch { /* ignore malformed */ }
    };
    ws.onclose = () => {
      if (this.ws !== ws) return;
      this.ws = null;
      this.o.onClose?.(this.opened);
      if (this.stopped) return;
      const d = this.delay;
      this.delay = Math.min(d * 2, this.o.maxDelay ?? 10000);
      this.timer = setTimeout(() => this.connect(), d);
    };
    ws.onerror = () => { /* close follows */ };
  }
}
