<template>
  <q-dialog v-model="settings.dialog">
    <q-card style="min-width: 320px; max-width: 95vw">
      <q-card-section class="text-h6">Settings</q-card-section>
      <q-card-section class="q-pt-none">
        <div v-if="settings.unauthorized" class="text-negative q-mb-sm">
          401: the server requires an access token. Enter it below.
        </div>
        <q-input v-model="draft" label="Access token (Bearer)" type="password" outlined dense autocomplete="off"
                 hint="Stored in this browser only. Leave empty when the service has no token." @keyup.enter="save" />
        <div class="q-mt-md">
          <q-btn-toggle v-model="settings.dark" no-caps unelevated toggle-color="primary" dense
                        :options="[{ label: 'Auto', value: 'auto' }, { label: 'Light', value: 'light' }, { label: 'Dark', value: 'dark' }]" />
        </div>
        <q-toggle v-model="settings.allowDelete" class="q-mt-sm" dense label="Allow deleting files on the phone (always asks first)" />
      </q-card-section>
      <q-card-actions align="right">
        <q-btn flat label="Close" v-close-popup />
        <q-btn color="primary" label="Save token" @click="save" />
      </q-card-actions>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import { useSettingsStore } from '@/stores/settings';

const settings = useSettingsStore();
const draft = ref(settings.token);
watch(() => settings.dialog, (o) => { if (o) draft.value = settings.token; });
function save(): void {
  settings.saveToken(draft.value);
  settings.dialog = false;
}
</script>
