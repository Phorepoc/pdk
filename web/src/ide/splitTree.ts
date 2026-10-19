// A split tree of tab groups, independent of any store: the Terminal panel's own splits (the editor area has the same
// shape in layout.ts). Tabs are string keys; the functions mutate the tree in place and return the (possibly new) root.

export type Dir = 'row' | 'column';
export type Zone = 'center' | 'left' | 'right' | 'top' | 'bottom';
export interface TGroup { kind: 'group'; id: number; tabs: string[]; active: string }
export interface TSplit { kind: 'split'; id: number; dir: Dir; children: TNode[]; sizes: number[] }
export type TNode = TGroup | TSplit;

export interface Tree { root: TNode; seq: number; focus: number }

export const newTree = (): Tree => ({ root: { kind: 'group', id: 1, tabs: [], active: '' }, seq: 1, focus: 1 });

export function groupsOf(n: TNode): TGroup[] {
  return n.kind === 'group' ? [n] : n.children.flatMap(groupsOf);
}

function parentOf(root: TNode, id: number): TSplit | undefined {
  if (root.kind !== 'split') return undefined;
  for (const c of root.children) {
    if (c.id === id) return root;
    const p = parentOf(c, id);
    if (p) return p;
  }
  return undefined;
}

/** Drop empty groups and one-child splits (an empty root group stays). */
export function tidy(t: Tree): void {
  const prune = (n: TNode): TNode | null => {
    if (n.kind === 'group') {
      if (!n.tabs.includes(n.active)) n.active = n.tabs[0] ?? '';
      return n.tabs.length ? n : null;
    }
    const kept: TNode[] = [];
    const sizes: number[] = [];
    n.children.forEach((c, i) => { const k = prune(c); if (k) { kept.push(k); sizes.push(n.sizes[i] ?? 1); } });
    if (!kept.length) return null;
    if (kept.length === 1) return kept[0]!;
    n.children = kept;
    n.sizes = sizes;
    return n;
  };
  t.root = prune(t.root) ?? { kind: 'group', id: ++t.seq, tabs: [], active: '' };
  if (!groupsOf(t.root).some((g) => g.id === t.focus)) t.focus = groupsOf(t.root)[0]!.id;
}

export function addTab(t: Tree, key: string, groupId = t.focus): void {
  const g = groupsOf(t.root).find((x) => x.id === groupId) ?? groupsOf(t.root)[0]!;
  g.tabs.push(key);
  g.active = key;
  t.focus = g.id;
}

export function removeTab(t: Tree, key: string): void {
  for (const g of groupsOf(t.root)) {
    const i = g.tabs.indexOf(key);
    if (i < 0) continue;
    g.tabs.splice(i, 1);
    if (g.active === key) g.active = g.tabs[Math.min(i, g.tabs.length - 1)] ?? '';
  }
  tidy(t);
}

/** A tab dropped on a group: the middle joins it, an edge splits that way. */
export function dropTab(t: Tree, key: string, targetId: number, zone: Zone): void {
  const from = groupsOf(t.root).find((g) => g.tabs.includes(key));
  const target = groupsOf(t.root).find((g) => g.id === targetId);
  if (!target) return;
  if (from && from.id === target.id && (zone === 'center' || from.tabs.length < 2)) { target.active = key; return; }
  if (from) { from.tabs.splice(from.tabs.indexOf(key), 1); if (from.active === key) from.active = from.tabs[0] ?? ''; }
  if (zone === 'center') {
    target.tabs.push(key);
    target.active = key;
    t.focus = target.id;
  } else {
    const dir: Dir = zone === 'left' || zone === 'right' ? 'row' : 'column';
    const before = zone === 'left' || zone === 'top';
    const ng: TGroup = { kind: 'group', id: ++t.seq, tabs: [key], active: key };
    const parent = parentOf(t.root, target.id);
    if (parent && parent.dir === dir) {
      const i = parent.children.findIndex((c) => c.id === target.id);
      const half = (parent.sizes[i] ?? 1) / 2;
      parent.sizes.splice(i, 1, half, half);
      parent.children.splice(before ? i : i + 1, 0, ng);
    } else {
      const sp: TSplit = { kind: 'split', id: ++t.seq, dir, children: before ? [ng, target] : [target, ng], sizes: [1, 1] };
      if (parent) parent.children.splice(parent.children.findIndex((c) => c.id === target.id), 1, sp);
      else t.root = sp;
    }
    t.focus = ng.id;
  }
  tidy(t);
}

/** Which zone of an element a drag is over: the outer quarter of each side is an edge. */
export function zoneAt(e: DragEvent): Zone {
  const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
  const x = (e.clientX - r.left) / r.width;
  const y = (e.clientY - r.top) / r.height;
  const edge = Math.min(x, 1 - x, y, 1 - y);
  if (edge > 0.25) return 'center';
  if (edge === x) return 'left';
  if (edge === 1 - x) return 'right';
  return edge === y ? 'top' : 'bottom';
}

/** The group at the top left (where a merged header starts) and the one at the top right (where it ends). */
export function cornerGroup(n: TNode, side: 'left' | 'right'): TGroup {
  while (n.kind === 'split') n = n.dir === 'row' && side === 'right' ? n.children[n.children.length - 1]! : n.children[0]!;
  return n;
}
