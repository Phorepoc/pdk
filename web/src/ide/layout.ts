import { defineStore } from 'pinia';
import { computed, reactive, ref, watch } from 'vue';
import { DOCKS, PANELS, isDynamic, type DockId } from './panels';

const KEY = 'phonelab.ide.layout.v3';

export type SideDock = 'left' | 'bottom' | 'right';
export type SplitDir = 'row' | 'column';
export type DropZone = 'center' | 'left' | 'right' | 'top' | 'bottom';

/** An editor group: a tab strip. */
export interface Group { kind: 'group'; id: number; tabs: string[]; active: string }
/** A split: children side by side (row) or stacked (column); sizes are percentages. */
export interface Split { kind: 'split'; id: number; dir: SplitDir; children: EditorNode[]; sizes: number[] }
export type EditorNode = Group | Split;

export interface LayoutState {
  /** which dock each panel lives in */
  where: Record<string, DockId>;
  /** the panel currently open in each side dock ('' = collapsed) */
  open: Record<SideDock, string>;
  sizes: { left: number; right: number; bottom: number };
  /** the editor area: a tree of splits with tab groups as leaves */
  editor: EditorNode;
  /** the group new tabs open in */
  focus: number;
  seq: number;
}

export function defaults(): LayoutState {
  const where: Record<string, DockId> = {};
  for (const p of PANELS) where[p.id] = p.dock;
  return {
    where,
    open: { left: 'files', bottom: 'livelog', right: '' },
    sizes: { left: 260, right: 380, bottom: 260 },
    editor: { kind: 'group', id: 1, tabs: ['live'], active: 'live' },
    focus: 1,
    seq: 1,
  };
}

export function groups(n: EditorNode): Group[] {
  return n.kind === 'group' ? [n] : n.children.flatMap(groups);
}

function load(): LayoutState {
  const d = defaults();
  try {
    const s = JSON.parse(localStorage.getItem(KEY) ?? 'null') as Partial<LayoutState> | null;
    if (!s || !s.editor) return d;
    const where = { ...d.where };
    for (const [k, v] of Object.entries(s.where ?? {})) if (k in where && DOCKS.includes(v)) where[k] = v;
    const st: LayoutState = {
      where,
      open: { ...d.open, ...(s.open ?? {}) },
      sizes: { ...d.sizes, ...(s.sizes ?? {}) },
      editor: s.editor,
      focus: s.focus ?? 1,
      seq: s.seq ?? 1,
    };
    for (const k of ['left', 'bottom', 'right'] as const) if (st.open[k] && st.where[st.open[k]] !== k) st.open[k] = '';
    return st;
  } catch { return d; }
}

/** The IDE workspace model: docks, open tool windows, the editor split tree. Persisted to localStorage. */
export const useIdeStore = defineStore('ide', () => {
  const st = reactive<LayoutState>(load());
  const bottomMax = ref(false);
  /** the tab being dragged (drop zones show while set) */
  const dragTab = ref('');

  watch(st, (v) => { try { localStorage.setItem(KEY, JSON.stringify(v)); } catch { /* ignore */ } }, { deep: true });

  const panelsIn = (dock: DockId) => PANELS.filter((p) => st.where[p.id] === dock).map((p) => p.id);
  const allGroups = () => groups(st.editor);
  const groupOf = (tab: string) => allGroups().find((g) => g.tabs.includes(tab));
  const isEditorTab = (id: string) => isDynamic(id) || st.where[id] === 'editor';

  function parentOf(node: EditorNode, root: EditorNode = st.editor): Split | undefined {
    if (root.kind !== 'split') return undefined;
    for (const c of root.children) {
      if (c.id === node.id) return root;
      const p = parentOf(node, c);
      if (p) return p;
    }
    return undefined;
  }

  /** Drop empty groups and one-child splits (the last group always stays). */
  function tidy(): void {
    const prune = (n: EditorNode): EditorNode | null => {
      if (n.kind === 'group') {
        n.tabs = n.tabs.filter(isEditorTab);
        if (!n.tabs.includes(n.active)) n.active = n.tabs[0] ?? '';
        return n.tabs.length ? n : null;
      }
      const kept: EditorNode[] = [];
      const sizes: number[] = [];
      n.children.forEach((c, i) => { const k = prune(c); if (k) { kept.push(k); sizes.push(n.sizes[i] ?? 0); } });
      if (!kept.length) return null;
      if (kept.length === 1) return kept[0]!;
      const sum = sizes.reduce((a, b) => a + b, 0) || 1;
      n.children = kept;
      n.sizes = sizes.map((s) => (s / sum) * 100);
      return n;
    };
    const r = prune(st.editor);
    st.editor = r ?? { kind: 'group', id: ++st.seq, tabs: [], active: '' };
    if (!allGroups().some((g) => g.id === st.focus)) st.focus = allGroups()[0]!.id;
  }

  function openTab(id: string, groupId = st.focus): void {
    const owner = groupOf(id);
    const g = owner ?? allGroups().find((x) => x.id === groupId) ?? allGroups()[0]!;
    if (!owner) g.tabs.push(id);
    g.active = id;
    st.focus = g.id;
  }

  /** Strip click: toggle a tool window; an editor panel is opened as a tab. */
  function activate(id: string): void {
    const dock = st.where[id]!;
    if (dock === 'editor') { openTab(id); return; }
    st.open[dock] = st.open[dock] === id ? '' : id;
  }

  function closeTab(id: string): void {
    for (const g of allGroups()) {
      const i = g.tabs.indexOf(id);
      if (i < 0) continue;
      g.tabs.splice(i, 1);
      if (g.active === id) g.active = g.tabs[Math.min(i, g.tabs.length - 1)] ?? '';
    }
    tidy();
  }

  function move(id: string, dock: DockId): void {
    const from = st.where[id]!;
    if (from === dock) return;
    if (from === 'editor') closeTab(id); else if (st.open[from] === id) st.open[from] = '';
    st.where[id] = dock;
    if (dock === 'editor') openTab(id); else st.open[dock] = id;
    tidy();
  }

  function hide(dock: SideDock): void { st.open[dock] = ''; }

  /** Where a dragged tab is dropped on a group: its middle (join the group) or one of its edges (split that way). */
  function dropTab(id: string, targetId: number, zone: DropZone): void {
    const from = groupOf(id);
    const target = allGroups().find((g) => g.id === targetId);
    if (!target) return;
    if (zone === 'center') {
      if (from && from.id === target.id) { target.active = id; return; }
      if (from) { from.tabs.splice(from.tabs.indexOf(id), 1); if (from.active === id) from.active = from.tabs[0] ?? ''; }
      target.tabs.push(id);
      target.active = id;
      st.focus = target.id;
      tidy();
      return;
    }
    // an edge: a group's only tab cannot be split off its own group
    if (from && from.id === target.id && from.tabs.length < 2) return;
    if (from) { from.tabs.splice(from.tabs.indexOf(id), 1); if (from.active === id) from.active = from.tabs[0] ?? ''; }
    const dir: SplitDir = zone === 'left' || zone === 'right' ? 'row' : 'column';
    const before = zone === 'left' || zone === 'top';
    const ng: Group = { kind: 'group', id: ++st.seq, tabs: [id], active: id };
    const parent = parentOf(target);
    if (parent && parent.dir === dir) {
      const i = parent.children.findIndex((c) => c.id === target.id);
      const half = (parent.sizes[i] ?? 50) / 2;
      parent.sizes.splice(i, 1, half, half);
      parent.children.splice(before ? i : i + 1, 0, ng);
    } else {
      const sp: Split = { kind: 'split', id: ++st.seq, dir, children: before ? [ng, target] : [target, ng], sizes: [50, 50] };
      if (parent) parent.children.splice(parent.children.findIndex((c) => c.id === target.id), 1, sp);
      else st.editor = sp;
    }
    st.focus = ng.id;
    tidy();
  }

  /** Split: move tab id into a new group beside its own (row = right, column = below). */
  function splitTab(id: string, dir: SplitDir = 'row'): void {
    const g = groupOf(id);
    if (g) dropTab(id, g.id, dir === 'row' ? 'right' : 'bottom');
  }

  /** Move a tab into another existing group. */
  function moveTab(id: string, toGroup: number): void { dropTab(id, toGroup, 'center'); }

  function reset(): void { Object.assign(st, defaults()); }

  const shown = computed(() => {
    const s = new Set<string>();
    for (const d of ['left', 'bottom', 'right'] as const) if (st.open[d]) s.add(st.open[d]);
    for (const g of groups(st.editor)) if (g.active) s.add(g.active);
    return s;
  });
  const groupCount = computed(() => groups(st.editor).length);

  return { st, bottomMax, dragTab, panelsIn, activate, openTab, closeTab, move, hide, splitTab, moveTab, dropTab, reset, shown, groupCount };
});
