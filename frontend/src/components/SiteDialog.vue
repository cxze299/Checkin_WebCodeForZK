<script setup>
import { nextTick, ref, watch } from 'vue';
import AppIcon from './AppIcon.vue';
import { dialogState, resolveDialog } from '../ui/dialog';

const input = ref(null);
const panel = ref(null);
watch(() => dialogState.open, async (open) => {
  if (open) {
    await nextTick();
    if (dialogState.mode === 'prompt') {
      input.value?.focus();
      input.value?.select();
    } else {
      panel.value?.focus();
    }
  }
});
</script>

<template>
  <Transition name="ios-dialog">
    <div v-if="dialogState.open" class="site-dialog-backdrop" @click.self="resolveDialog(false)">
      <section ref="panel" class="site-dialog" :class="`tone-${dialogState.tone}`" role="alertdialog" aria-modal="true" aria-labelledby="site-dialog-title" tabindex="-1" @keydown.esc="resolveDialog(false)">
        <span class="site-dialog-icon"><AppIcon :name="dialogState.tone === 'danger' ? 'warning' : 'check'" :size="25" /></span>
        <div class="site-dialog-copy">
          <h2 id="site-dialog-title">{{ dialogState.title }}</h2>
          <p v-if="dialogState.message">{{ dialogState.message }}</p>
        </div>
        <input v-if="dialogState.mode === 'prompt'" ref="input" v-model="dialogState.value" :placeholder="dialogState.placeholder" @keydown.enter.prevent="resolveDialog(true)" />
        <footer>
          <button class="ios-secondary-button" type="button" @click="resolveDialog(false)">{{ dialogState.cancelLabel }}</button>
          <button :class="{ 'ios-danger-button': dialogState.tone === 'danger' }" type="button" @click="resolveDialog(true)">{{ dialogState.confirmLabel }}</button>
        </footer>
      </section>
    </div>
  </Transition>
</template>
