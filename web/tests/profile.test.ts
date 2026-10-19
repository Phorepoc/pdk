import { afterEach, describe, expect, it, vi } from 'vitest';
import { h } from 'vue';
import { QLayout, QPageContainer } from 'quasar';
import { flushPromises, mount } from '@vue/test-utils';
import { createPinia, setActivePinia } from 'pinia';
import { SampleHistory, samplesToCsv, sortProcs, type ProcInfo, type ProfileResult } from '@/api/profile';
import ProfilePage from '@/pages/ProfilePage.vue';
import { useDevicesStore } from '@/stores/devices';

const proc = (name: string, cpu: number, ram: number, threads = 1, chunks = 1): ProcInfo => ({ id: 1, name, cpu, ram, threads, chunks });
const sample = (cpu = 50, procs: ProcInfo[] = [proc('Telephone', 25, 1000)]): ProfileResult => ({
  window_s: 1, cores: 4, core_busy: [cpu, 0, 0, 0], cpu_total: cpu / 4, free_ram: 5000, kernel_heap: 300,
  global_ram: 9000, processes: procs, threads: [{ id: 1, process: 'Telephone', name: 'main', cpu: 20 }, { id: 2, process: 'eshell', name: 'sh', cpu: 5 }],
});

describe('SampleHistory', () => {
  it('caps by count', () => {
    const hs = new SampleHistory(1e12, 5);
    for (let i = 0; i < 12; i++) hs.add(sample(i), i);
    expect(hs.length).toBe(5);
    expect(hs.samples[0]!.t).toBe(7);
  });
  it('drops samples older than the window but keeps the newest', () => {
    const hs = new SampleHistory(1000, 100);
    hs.add(sample(), 0); hs.add(sample(), 500); hs.add(sample(), 5000);
    expect(hs.samples.map((s) => s.t)).toEqual([5000]);
  });
  it('builds per-process series with zero for absent samples', () => {
    const hs = new SampleHistory();
    hs.add(sample(1, [proc('a', 10, 1)]), 1); hs.add(sample(1, []), 2);
    expect(hs.procSeries('a', (p) => p.cpu)).toEqual([10, 0]);
  });
});

describe('sortProcs', () => {
  const l = [proc('b', 10, 5), proc('a', 10, 9), proc('c', 30, 1)];
  it('defaults to cpu desc then ram desc', () => {
    expect(sortProcs(l).map((p) => p.name)).toEqual(['c', 'a', 'b']);
  });
  it('sorts by name and ram, either direction, without mutating', () => {
    expect(sortProcs(l, { key: 'name', dir: 1 }).map((p) => p.name)).toEqual(['a', 'b', 'c']);
    expect(sortProcs(l, { key: 'ram', dir: 1 }).map((p) => p.name)).toEqual(['c', 'b', 'a']);
    expect(l[0]!.name).toBe('b');
  });
});

describe('CSV export', () => {
  it('writes a header and one row per sample and process, quoting commas', () => {
    const csv = samplesToCsv([{ t: 0, p: sample(40, [proc('a,b', 1.5, 100, 2, 3), proc('c', 2, 3)]) }]);
    const lines = csv.trim().split('\n');
    expect(lines[0]).toBe('time_iso,process,cpu_pct,ram_bytes,threads,chunks,cpu_total_pct,free_ram,kernel_heap');
    expect(lines).toHaveLength(3);
    expect(lines[1]).toBe('1970-01-01T00:00:00.000Z,"a,b",1.5,100,2,3,10,5000,300');
  });
});

describe('ProfilePage', () => {
  afterEach(() => { vi.unstubAllGlobals(); });
  it('polls the profile op with the live caller header and renders tables', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ result: sample(50, [proc('Telephone', 25, 2 * 1048576, 3, 4), proc('eshell', 10, 1048576)]) }), { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    const pinia = createPinia();
    setActivePinia(pinia);
    const devices = useDevicesStore();
    devices.list = [{ id: 'pp', driver: 'p', health: { device: 'pp', driver: 'p', transport: 't', state: 'connected', last_byte_age_s: 0, reconnects: 0, boot: 1, lines: 0 },
      ops: [{ name: 'profile', description: '', capability: 'profile', reboots: false, params: [] }] }];
    const w = mount({ render: () => h(QLayout, () => h(QPageContainer, () => h(ProfilePage))) }, { global: { plugins: [pinia] } });
    await vi.waitFor(() => expect(w.text()).toContain('Telephone'));
    await flushPromises();
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/devices/pp/ops/profile');
    expect((init.headers as Record<string, string>)['X-Phonelab-Caller']).toBe('live');
    expect(w.findAll('[data-row="proc"]').map((r) => r.find('td').text())).toEqual(['Telephone', 'eshell']);
    expect(w.text()).toContain('2.0 MiB');
    expect(w.findAll('[data-row="thread"]')).toHaveLength(2);
    await w.findAll('[data-row="proc"]')[1]!.trigger('click'); // filter threads to eshell
    expect(w.findAll('[data-row="thread"]')).toHaveLength(1);
    w.unmount();
  });
});
