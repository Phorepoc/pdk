<template>
  <div class="ide" data-test="ide">
    <div class="ide-twhead" style="height: 32px; padding-left: 10px">
      <q-icon name="smartphone" size="18px" color="primary" />
      <span class="title" style="flex: none; margin-right: 12px">PhoneLab</span>
      <q-select
        v-if="devices.list.length > 1" v-model="settings.device" :options="devices.list.map((d) => d.id)"
        dense borderless dark options-dark style="min-width: 110px"
      />
      <span v-else class="text-caption" style="font-weight: normal">{{ devices.currentId }}</span>
      <div style="flex: 1" />
      <q-banner v-if="settings.unauthorized" dense class="bg-negative text-white q-py-none">401: access token needed</q-banner>
      <q-banner v-else-if="devices.error" dense class="bg-warning text-black q-py-none">{{ devices.error }}</q-banner>
    </div>

    <div class="ide-body">
      <div class="ide-strip" data-test="strip-left">
        <ide-strip-button v-for="p in topPanels" :key="p.id" :panel="p" />
        <div class="spacer" />
        <ide-strip-button v-for="p in bottomPanels" :key="p.id" :panel="p" />
      </div>

      <!-- the bottom dock spans the full width (as in JetBrains IDEs); left | editor | right sit above it -->
      <div class="ide-center"><div class="absolute-full">
        <dock-split :show="!!ide.st.open.bottom && !ide.bottomMax" v-model:size="ide.st.sizes.bottom" horizontal>
          <template #dock><tool-window dock="bottom" :id="ide.st.open.bottom" /></template>
          <template #main>
            <tool-window v-if="ide.bottomMax && ide.st.open.bottom" dock="bottom" :id="ide.st.open.bottom" />
            <dock-split v-else :show="!!ide.st.open.left" v-model:size="ide.st.sizes.left" dock-first>
              <template #dock><tool-window dock="left" :id="ide.st.open.left" /></template>
              <template #main>
                <dock-split :show="!!ide.st.open.right" v-model:size="ide.st.sizes.right">
                  <template #dock><tool-window dock="right" :id="ide.st.open.right" /></template>
                  <template #main><editor-area /></template>
                </dock-split>
              </template>
            </dock-split>
          </template>
        </dock-split>
        </div>
      </div>

      <div v-if="rightPanels.length" class="ide-strip right" data-test="strip-right">
        <ide-strip-button v-for="p in rightPanels" :key="p.id" :panel="p" />
      </div>
    </div>

    <status-bar />
    <settings-dialog />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import SettingsDialog from '@/components/SettingsDialog.vue';
import { useAppBoot } from '@/composables/useAppBoot';
import { useDevicesStore } from '@/stores/devices';
import { useSettingsStore } from '@/stores/settings';
import DockSplit from './DockSplit.vue';
import EditorArea from './EditorArea.vue';
import IdeStripButton from './IdeStripButton.vue';
import StatusBar from './StatusBar.vue';
import ToolWindow from './ToolWindow.vue';
import { useIdeStore } from './layout';
import { PANELS } from './panels';

useAppBoot(true);
const settings = useSettingsStore();
const devices = useDevicesStore();
const ide = useIdeStore();

const inDock = (...d: string[]) => PANELS.filter((p) => d.includes(ide.st.where[p.id]!));
const topPanels = computed(() => inDock('left'));
const bottomPanels = computed(() => inDock('bottom'));
const rightPanels = computed(() => inDock('right'));

</script>
