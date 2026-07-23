<script setup>
import { computed, ref, watch } from 'vue';
import AppIcon from './AppIcon.vue';

const props = defineProps({
  modelValue: { type: String, default: '' },
  disabled: { type: Boolean, default: false },
  label: { type: String, default: '选择日期' },
});
const emit = defineEmits(['update:modelValue']);
function localDateString(date = new Date()) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
}
const open = ref(false);
const viewMonth = ref((props.modelValue || localDateString()).slice(0, 7));
watch(() => props.modelValue, (value) => { if (value) viewMonth.value = value.slice(0, 7); });

const monthLabel = computed(() => {
  const [year, month] = viewMonth.value.split('-').map(Number);
  return `${year} 年 ${month} 月`;
});
const days = computed(() => {
  const [year, month] = viewMonth.value.split('-').map(Number);
  const offset = new Date(year, month - 1, 1).getDay();
  const count = new Date(year, month, 0).getDate();
  return [...Array(offset).fill(null), ...Array.from({ length: count }, (_, index) => index + 1)];
});

function shift(delta) {
  const [year, month] = viewMonth.value.split('-').map(Number);
  const date = new Date(year, month - 1 + delta, 1);
  viewMonth.value = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}`;
}
function choose(day) {
  if (!day) return;
  emit('update:modelValue', `${viewMonth.value}-${String(day).padStart(2, '0')}`);
  open.value = false;
}
function chooseToday() {
  const today = localDateString();
  emit('update:modelValue', today);
  viewMonth.value = today.slice(0, 7);
  open.value = false;
}
</script>

<template>
  <button class="app-date-field" type="button" :disabled="disabled" :aria-label="label" @click="open = true">
    <AppIcon name="calendar" :size="18" /><span :class="{ placeholder: !modelValue }">{{ modelValue || label }}</span><AppIcon name="chevron" :size="14" />
  </button>
  <Teleport to="body">
    <Transition name="ios-dialog">
      <div v-if="open" class="site-dialog-backdrop date-picker-backdrop" @click.self="open = false">
        <section class="app-date-picker" role="dialog" aria-modal="true" :aria-label="label" @keydown.esc="open = false">
          <header><div><small>SELECT DATE</small><h2>{{ label }}</h2></div><button class="ios-text-button" type="button" @click="open = false">关闭</button></header>
          <div class="app-date-switcher"><button type="button" aria-label="上个月" @click="shift(-1)">‹</button><strong>{{ monthLabel }}</strong><button type="button" aria-label="下个月" @click="shift(1)">›</button></div>
          <div class="app-date-weekdays" aria-hidden="true"><span>日</span><span>一</span><span>二</span><span>三</span><span>四</span><span>五</span><span>六</span></div>
          <div class="app-date-grid"><button v-for="(day,index) in days" :key="index" :disabled="!day" :class="{ selected: day && `${viewMonth}-${String(day).padStart(2,'0')}` === modelValue, today: day && `${viewMonth}-${String(day).padStart(2,'0')}` === localDateString() }" type="button" @click="choose(day)">{{ day || '' }}</button></div>
          <footer><button class="ios-secondary-button" type="button" @click="chooseToday">今天</button><button v-if="modelValue" class="ios-text-button" type="button" @click="emit('update:modelValue',''); open = false">清除</button></footer>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>
