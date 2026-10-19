<template>
  <q-scroll-area class="fit">
    <div class="row q-pa-md q-gutter-lg items-start">
      <phone-keypad />
      <live-settings />
      <input-script />
    </div>
  </q-scroll-area>
</template>

<script setup lang="ts">
// The keypad and the live settings as their own tab, so the Live tab can show only the screen.
import InputScript from './InputScript.vue';
import LiveSettings from './LiveSettings.vue';
import PhoneKeypad from './PhoneKeypad.vue';
</script>
