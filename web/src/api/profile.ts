// Profile op result, the in-memory sample history, sorting and CSV export (no DOM).

export interface ProcInfo { id: number; name: string; cpu: number; ram: number; threads: number; chunks: number }
export interface ThreadInfo { id: number; process: string; name: string; cpu: number }
export interface ProfileResult {
  window_s: number;
  cores: number;
  core_busy: number[];
  cpu_total: number;
  free_ram: number;
  kernel_heap: number;
  global_ram: number;
  processes: ProcInfo[];
  threads: ThreadInfo[];
  snapshot_bytes?: number;
}

export interface Sample { t: number; p: ProfileResult }

export const HISTORY_MS = 10 * 60 * 1000;

/** Time-bounded + count-bounded ring of samples. */
export class SampleHistory {
  samples: Sample[] = [];
  constructor(public maxAgeMs = HISTORY_MS, public maxCount = 600) {}

  add(p: ProfileResult, t = Date.now()): void {
    this.samples.push({ t, p });
    const cut = t - this.maxAgeMs;
    let drop = 0;
    while (drop < this.samples.length - 1 && this.samples[drop]!.t < cut) drop++;
    if (this.samples.length - drop > this.maxCount) drop = this.samples.length - this.maxCount;
    if (drop > 0) this.samples.splice(0, drop);
  }
  clear(): void { this.samples = []; }
  get length(): number { return this.samples.length; }
  get last(): Sample | undefined { return this.samples[this.samples.length - 1]; }

  series(f: (p: ProfileResult) => number): number[] { return this.samples.map((s) => f(s.p)); }
  coreSeries(core: number): number[] { return this.samples.map((s) => s.p.core_busy?.[core] ?? 0); }
  procSeries(name: string, f: (p: ProcInfo) => number): number[] {
    return this.samples.map((s) => { const q = s.p.processes?.find((x) => x.name === name); return q ? f(q) : 0; });
  }
  sumRam(p: ProfileResult): number { return (p.processes ?? []).reduce((a, q) => a + q.ram, 0); }
}

export type ProcKey = 'name' | 'cpu' | 'ram' | 'threads' | 'chunks';
export interface SortSpec { key: ProcKey; dir: 1 | -1 }

/** Sorts a copy by the chosen column; ties fall back to cpu desc, ram desc, name. */
export function sortProcs(list: ProcInfo[], s: SortSpec = { key: 'cpu', dir: -1 }): ProcInfo[] {
  const cmp = (a: ProcInfo, b: ProcInfo): number => {
    const v = s.key === 'name' ? a.name.localeCompare(b.name) : (a[s.key] as number) - (b[s.key] as number);
    if (v) return v * s.dir;
    return (b.cpu - a.cpu) || (b.ram - a.ram) || a.name.localeCompare(b.name);
  };
  return list.slice().sort(cmp);
}

export function fmtBytes(n: number): string {
  if (!isFinite(n)) return '-';
  if (n < 1024) return `${n} B`;
  if (n < 1048576) return `${(n / 1024).toFixed(1)} KiB`;
  if (n < 1073741824) return `${(n / 1048576).toFixed(1)} MiB`;
  return `${(n / 1073741824).toFixed(2)} GiB`;
}

function csvCell(v: string | number): string {
  const s = String(v);
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/** One row per (sample, process), plus the global figures repeated on each row. */
export function samplesToCsv(samples: Sample[]): string {
  const head = ['time_iso', 'process', 'cpu_pct', 'ram_bytes', 'threads', 'chunks', 'cpu_total_pct', 'free_ram', 'kernel_heap'];
  const rows = [head.join(',')];
  for (const s of samples) {
    const iso = new Date(s.t).toISOString();
    for (const q of s.p.processes ?? []) {
      rows.push([iso, q.name, q.cpu, q.ram, q.threads, q.chunks, s.p.cpu_total, s.p.free_ram, s.p.kernel_heap].map(csvCell).join(','));
    }
  }
  return rows.join('\n') + '\n';
}
