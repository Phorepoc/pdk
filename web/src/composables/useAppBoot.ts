import { onBeforeUnmount, onMounted, watch } from 'vue';
import { useQuasar } from 'quasar';
import { api, onUnauthorized } from '@/api/client';
import { useDevicesStore } from '@/stores/devices';
import { useLiveStore } from '@/stores/live';
import { useSettingsStore } from '@/stores/settings';

/** App start-up: load devices, start the live socket, react to auth/theme. */
export function useAppBoot(forceDark = false): void {
  const $q = useQuasar();
  const settings = useSettingsStore();
  const devices = useDevicesStore();
  const live = useLiveStore();

  async function boot(): Promise<void> {
    await devices.load();
    try { devices.version = (await api.health()).version; } catch { /* shown by banner */ }
    if (devices.currentId) void live.seed(devices.currentId);
  }

  watch(() => settings.dark, (v) => $q.dark.set(forceDark && v === 'auto' ? true : v === 'auto' ? 'auto' : v === 'dark'), { immediate: true });
  onMounted(() => {
    onUnauthorized(() => { settings.unauthorized = true; settings.dialog = true; });
    void boot();
    live.start();
  });
  onBeforeUnmount(() => { onUnauthorized(null); live.stop(); });
  watch(() => settings.token, () => { void boot(); live.restart(); });
  watch(() => devices.currentId, (id) => { if (id) void live.seed(id); });
}
