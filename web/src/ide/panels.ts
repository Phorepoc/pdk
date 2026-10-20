// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

import { defineAsyncComponent, type Component } from 'vue';

export type DockId = 'left' | 'bottom' | 'right' | 'editor';
export const DOCKS: DockId[] = ['left', 'bottom', 'right', 'editor'];

export interface PanelDef {
  id: string;
  title: string;
  icon: string;
  dock: DockId; // default home
  component: Component;
  /** wrap in a q-layout container (pages built on q-page need one) */
  page?: boolean;
  /** passed to the component as the `arg` prop (dynamic panels: the file path) */
  arg?: string;
  /** the panel draws the tool window's title and actions itself (the Terminal: merged into its tab strips) */
  ownHeader?: boolean;
}

const lazy = (loader: () => Promise<unknown>) => defineAsyncComponent(loader as () => Promise<Component>);

// Left dock: tool windows that replace each other at the top left (the strip's top group).
// Bottom dock: full-width tool windows (the strip's bottom group). Editor: views that open as tabs (listed by "Views").
export const PANELS: PanelDef[] = [
  { id: 'files', title: 'Project', icon: 'folder_open', dock: 'left', component: lazy(() => import('@/ide/FileBrowserPanel.vue')) },
  { id: 'device', title: 'Device', icon: 'smartphone', dock: 'left', component: lazy(() => import('@/components/DevicePanel.vue')) },
  { id: 'views', title: 'Views', icon: 'dashboard_customize', dock: 'left', component: lazy(() => import('@/ide/ViewsPanel.vue')) },
  { id: 'hotspots', title: 'Hotspots', icon: 'local_fire_department', dock: 'left', component: lazy(() => import('@/components/HotspotsTool.vue')) },
  { id: 'live', title: 'Live', icon: 'cast_connected', dock: 'editor', component: lazy(() => import('@/live/LiveScreen.vue')) },
  { id: 'controls', title: 'Phone controls', icon: 'dialpad', dock: 'editor', component: lazy(() => import('@/live/PhoneControls.vue')) },
  { id: 'ops', title: 'Ops console', icon: 'build', dock: 'editor', page: true, component: lazy(() => import('@/pages/OpsPage.vue')) },
  { id: 'profile', title: 'Profile', icon: 'speed', dock: 'editor', page: true, component: lazy(() => import('@/pages/ProfilePage.vue')) },
  { id: 'push', title: 'Push image', icon: 'upload', dock: 'editor', page: true, component: lazy(() => import('@/pages/PushPage.vue')) },
  { id: 'boots', title: 'Boots', icon: 'restart_alt', dock: 'editor', component: lazy(() => import('@/pages/BootsPage.vue')) },
  { id: 'screenshots', title: 'Screenshots', icon: 'photo_camera', dock: 'editor', page: true, component: lazy(() => import('@/pages/ScreenshotsPage.vue')) },
  { id: 'livelog', title: 'Live log', icon: 'receipt_long', dock: 'bottom', component: lazy(() => import('@/components/LiveLogPanel.vue')) },
  { id: 'log', title: 'Log history', icon: 'article', dock: 'bottom', page: true, component: lazy(() => import('@/pages/LogHistoryPage.vue')) },
  { id: 'timeline', title: 'Timeline', icon: 'timeline', dock: 'bottom', page: true, component: lazy(() => import('@/pages/TimelinePage.vue')) },
  { id: 'terminal', title: 'Terminal', icon: 'terminal', dock: 'bottom', ownHeader: true, component: lazy(() => import('@/ide/TerminalPanel.vue')) },
];

const fileView = lazy(() => import('@/ide/FileView.vue'));

/** Dynamic editor tabs: "file:<path>" = a file on the phone. */
export const isDynamic = (id: string): boolean => id.startsWith('file:');
export const fileTab = (path: string): string => `file:${path}`;

export const panelById = (id: string): PanelDef | undefined => {
  if (id.startsWith('file:')) {
    const path = id.slice(5);
    return { id, title: path.split('/').pop() || path, icon: 'description', dock: 'editor', component: fileView, arg: path };
  }
  return PANELS.find((p) => p.id === id);
};
