export const BAD_LINE = /Fault|panic|FAILED/i;

export function fmtDur(sec: number): string {
  if (!isFinite(sec) || sec < 0) return '-';
  const s = Math.floor(sec);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const r = s % 60;
  if (h) return `${h}h ${String(m).padStart(2, '0')}m`;
  if (m) return `${m}m ${String(r).padStart(2, '0')}s`;
  return `${r}s`;
}

export function fmtTs(ts: number | string | undefined): string {
  if (ts === undefined || ts === '') return '';
  const d = new Date(ts);
  return isNaN(d.getTime()) ? String(ts) : d.toLocaleString();
}

export function fmtTime(ts: number): string {
  const d = new Date(ts);
  return d.toLocaleTimeString([], { hour12: false }) + '.' + String(d.getMilliseconds()).padStart(3, '0');
}

export function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1048576) return `${(n / 1024).toFixed(1)} KiB`;
  return `${(n / 1048576).toFixed(2)} MiB`;
}

export function pretty(v: unknown): string {
  try { return JSON.stringify(v, null, 2) ?? ''; } catch { return String(v); }
}

export function toNum(v: unknown): number {
  if (typeof v === 'number') return v;
  const s = String(v ?? '').trim();
  return s ? Number(s) : NaN;
}

export function hex32(v: unknown): string {
  const n = toNum(v);
  return isNaN(n) ? String(v) : '0x' + (n >>> 0).toString(16).padStart(8, '0');
}

export function downloadText(name: string, text: string): void {
  const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
  const a = document.createElement('a');
  a.href = url;
  a.download = name;
  a.click();
  URL.revokeObjectURL(url);
}
