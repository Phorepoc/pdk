import { defineStore } from 'pinia';
import { ref, watch } from 'vue';
import { getToken, loadStoredToken, setToken } from '@/api/client';

export type DarkMode = 'auto' | 'light' | 'dark';
const DARK_KEY = 'phonelab.dark';
const DEV_KEY = 'phonelab.device';
const DEL_KEY = 'phonelab.allowDelete';

function read(k: string): string {
  try { return localStorage.getItem(k) ?? ''; } catch { return ''; }
}
function write(k: string, v: string): void {
  try { localStorage.setItem(k, v); } catch { /* ignore */ }
}

export const useSettingsStore = defineStore('settings', () => {
  const d = read(DARK_KEY);
  const dark = ref<DarkMode>(d === 'light' || d === 'dark' ? d : 'auto');
  const device = ref(read(DEV_KEY));
  /** the Project panel and file tabs offer Delete (always with a confirmation) */
  const allowDelete = ref(read(DEL_KEY) !== '0');
  const token = ref(loadStoredToken());
  const unauthorized = ref(false);
  const dialog = ref(false);
  setToken(token.value);

  watch(dark, (v) => write(DARK_KEY, v));
  watch(device, (v) => write(DEV_KEY, v));
  watch(allowDelete, (v) => write(DEL_KEY, v ? '1' : '0'));

  function saveToken(t: string): void {
    setToken(t);
    token.value = getToken();
    unauthorized.value = false;
  }

  return { dark, device, allowDelete, token, unauthorized, dialog, saveToken };
});
