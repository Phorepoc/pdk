import { defineStore } from 'pinia';
import { ref } from 'vue';

export interface Transfer { id: number; label: string; done: number; total: number; error?: string }

/** Uploads in flight (image blobs, files to the phone); the status bar shows each with its progress. */
export const useTransfersStore = defineStore('transfers', () => {
  const list = ref<Transfer[]>([]);
  let seq = 0;

  function begin(label: string, total = 0): Transfer {
    const t: Transfer = { id: ++seq, label, done: 0, total };
    list.value.push(t);
    return list.value[list.value.length - 1]!;
  }
  function progress(t: Transfer, done: number, total: number): void { t.done = done; t.total = total; }
  /** Finished: drop it (a failure stays on the bar a few seconds). */
  function end(t: Transfer, error?: string): void {
    const drop = () => { list.value = list.value.filter((x) => x.id !== t.id); };
    if (error) { t.error = error; setTimeout(drop, 6000); } else drop();
  }
  /** Wrap an upload: progress callback in, the bar entry out. */
  async function track<T>(label: string, run: (onProgress: (done: number, total: number) => void) => Promise<T>): Promise<T> {
    const t = begin(label);
    try {
      const r = await run((d, n) => progress(t, d, n));
      end(t);
      return r;
    } catch (e) { end(t, (e as Error).message); throw e; }
  }

  return { list, begin, progress, end, track };
});
