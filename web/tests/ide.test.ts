import { beforeEach, describe, expect, it } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';
import { groups, useIdeStore } from '@/ide/layout';
import { fileTab } from '@/ide/panels';

describe('IDE layout store', () => {
  beforeEach(() => { localStorage.clear(); setActivePinia(createPinia()); });

  const tabsOf = (ide: ReturnType<typeof useIdeStore>) => groups(ide.st.editor).map((g) => g.tabs);

  it('starts with Project open on the left, the live log at the bottom and Live as a tab', () => {
    const ide = useIdeStore();
    expect(ide.st.open.left).toBe('files');
    expect(ide.st.open.bottom).toBe('livelog');
    expect(tabsOf(ide)).toEqual([['live']]);
  });

  it('toggles a tool window and opens editor panels as tabs', () => {
    const ide = useIdeStore();
    ide.activate('files');
    expect(ide.st.open.left).toBe('');
    ide.activate('device');
    expect(ide.st.open.left).toBe('device');
    ide.activate('ops');
    expect(groups(ide.st.editor)[0]!.active).toBe('ops');
  });

  it('moves a panel between docks', () => {
    const ide = useIdeStore();
    ide.openTab('ops');
    ide.move('live', 'right');
    expect(ide.st.where['live']).toBe('right');
    expect(ide.st.open.right).toBe('live');
    expect(tabsOf(ide).flat()).not.toContain('live');
    ide.move('live', 'editor');
    expect(tabsOf(ide).flat()).toContain('live');
  });

  it('splits right three ways, then down, and merges back when groups empty', () => {
    const ide = useIdeStore();
    for (const t of ['ops', 'profile', 'push']) ide.openTab(t);
    ide.splitTab('ops', 'row');
    ide.splitTab('profile', 'row');
    expect(ide.st.editor.kind).toBe('split');
    expect(ide.st.editor.kind === 'split' && ide.st.editor.dir).toBe('row');
    expect(ide.groupCount).toBe(3);
    ide.splitTab('push', 'column');
    expect(ide.groupCount).toBe(4);
    for (const t of ['ops', 'profile', 'push']) ide.closeTab(t);
    expect(ide.groupCount).toBe(1);
    expect(tabsOf(ide)).toEqual([['live']]);
  });

  it('opens files as dynamic tabs', () => {
    const ide = useIdeStore();
    ide.openTab(fileTab('/C:/data/a.txt'));
    expect(tabsOf(ide)[0]).toContain('file:/C:/data/a.txt');
    ide.closeTab('file:/C:/data/a.txt');
    expect(tabsOf(ide)[0]).toEqual(['live']);
  });

  it('drops tabs on edges and middles, without duplicate group ids', () => {
    const ide = useIdeStore();
    for (const t of ['ops', 'profile', 'push']) ide.openTab(t);
    const g1 = groups(ide.st.editor)[0]!.id;
    ide.dropTab('ops', g1, 'left');
    expect(ide.st.editor.kind === 'split' && ide.st.editor.dir).toBe('row');
    expect(tabsOf(ide)[0]).toEqual(['ops']);
    ide.dropTab('push', g1, 'bottom');
    expect(ide.groupCount).toBe(3);
    const ids = groups(ide.st.editor).map((g) => g.id);
    expect(new Set(ids).size).toBe(ids.length);
    const opsGroup = groups(ide.st.editor).find((g) => g.tabs.includes('ops'))!.id;
    ide.dropTab('push', opsGroup, 'center');
    expect(ide.groupCount).toBe(2);
    expect(groups(ide.st.editor).find((g) => g.id === opsGroup)!.tabs).toEqual(['ops', 'push']);
    ide.dropTab('live', g1, 'right');
    expect(ide.groupCount).toBe(3);
  });
});
