import { onBeforeUnmount, ref } from 'vue';

/** A ref of Date.now() ticking once a second, for "up for ..." and "last byte ... ago" displays. */
export function useNow() {
  const now = ref(Date.now());
  const t = setInterval(() => { now.value = Date.now(); }, 1000);
  onBeforeUnmount(() => clearInterval(t));
  return now;
}
