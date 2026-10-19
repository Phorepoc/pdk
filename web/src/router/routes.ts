import type { RouteRecordRaw } from 'vue-router';

const routes: RouteRecordRaw[] = [
  { path: '/', component: () => import('@/ide/IdeLayout.vue') },
  { path: '/:catchAll(.*)*', redirect: '/' },
];

export default routes;
