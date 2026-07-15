<script setup>
defineProps({ name: { type: String, default: 'circle' }, size: { type: Number, default: 20 } });

const paths = {
  home: ['M3 11.5 12 4l9 7.5', 'M5.5 10v10h13V10', 'M9.5 20v-6h5v6'],
  chart: ['M4 19V9', 'M10 19V5', 'M16 19v-7', 'M22 19H2'],
  library: ['M4 4h6v16H4z', 'M10 6h5v14h-5', 'm15 3 4-1 2 15-6 1z'],
  user: ['M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z', 'M4 21a8 8 0 0 1 16 0'],
  settings: ['M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7Z', 'M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2 3.46-.09-.03a1.7 1.7 0 0 0-1.8.22l-.7.41a1.7 1.7 0 0 0-.86 1.67V22h-4v-.1a1.7 1.7 0 0 0-.86-1.67l-.7-.41a1.7 1.7 0 0 0-1.8-.22l-.09.03-2-3.46.06-.06A1.7 1.7 0 0 0 4.6 15v-.82a1.7 1.7 0 0 0-1-1.56L3.5 12l2-3.46.1.03a1.7 1.7 0 0 0 1.8-.22l.7-.41A1.7 1.7 0 0 0 8.96 6.3V6h4v.1a1.7 1.7 0 0 0 .86 1.67l.7.41a1.7 1.7 0 0 0 1.8.22l.09-.03 2 3.46-.06.06a1.7 1.7 0 0 0-.34 1.88Z'],
  users: ['M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2', 'M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z', 'M22 21v-2a4 4 0 0 0-3-3.87', 'M16 3.13a4 4 0 0 1 0 7.75'],
  calendar: ['M5 3v3M19 3v3M3 9h18', 'M4 5h16a1 1 0 0 1 1 1v14H3V6a1 1 0 0 1 1-1Z', 'm8 15 2 2 5-5'],
  upload: ['M12 16V4', 'm7 9 5-5 5 5', 'M4 20h16'],
  database: ['M4 6c0 2 3.6 3 8 3s8-1 8-3-3.6-3-8-3-8 1-8 3Z', 'M4 6v6c0 2 3.6 3 8 3s8-1 8-3V6', 'M4 12v6c0 2 3.6 3 8 3s8-1 8-3v-6'],
  plus: ['M12 5v14M5 12h14'],
  chevron: ['m9 18 6-6-6-6'],
  logout: ['M10 17l5-5-5-5M15 12H3', 'M14 3h5a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-5'],
  menu: ['M4 7h16M4 12h16M4 17h16'],
  lock: ['M6 10h12v11H6z', 'M8 10V7a4 4 0 0 1 8 0v3'],
  check: ['m5 12 4 4L19 6'],
  file: ['M6 2h8l4 4v16H6z', 'M14 2v5h5'],
  play: ['M8 5v14l11-7Z'],
  refresh: ['M20 6v5h-5', 'M4 18v-5h5', 'M6.1 9a7 7 0 0 1 11.8-2.6L20 11', 'M4 13l2.1 4.6A7 7 0 0 0 18 15'],
  warning: ['M12 3 2.8 20h18.4Z', 'M12 9v4', 'M12 17h.01'],
  circle: ['M12 12h.01'],
};
</script>

<template>
  <svg :width="size" :height="size" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path v-for="path in (paths[name] || paths.circle)" :key="path" :d="path" />
  </svg>
</template>
