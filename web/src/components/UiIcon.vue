<script setup lang="ts">
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 20 })
const paths: Record<string, string> = {
  refresh: 'M20 7v5h-5 M4 17v-5h5 M5.5 8a7 7 0 0 1 11.7-3L20 8 M4 16l2.8 3A7 7 0 0 0 18.5 16',
  check: 'M5 12l4 4L19 6',
  layers: 'M12 3l10 5-10 5L2 8z M2 12l10 5 10-5 M2 16l10 5 10-5',
  stack: 'M4 4h16v4H4z M4 11h16v4H4z M4 18h16v3H4z',
  circle: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0 M12 8v4l3 2',
  activity: 'M3 12h4l3-8 4 16 3-8h4',
  flag: 'M5 21V4 M5 4c5-4 9 4 14 0v10c-5 4-9-4-14 0',

  members:
    'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2 M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8 M20 21v-2a4 4 0 0 0-3-3.9 M16 3.1a4 4 0 0 1 0 7.8',
  grid: 'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',
  ticket: 'M4 5h16v5a2 2 0 0 0 0 4v5H4v-5a2 2 0 0 0 0-4z M9 5v14',
  book: 'M4 4h7a3 3 0 0 1 3 3v14a4 4 0 0 0-4-2H4z M14 7a3 3 0 0 1 3-3h3v15h-3a4 4 0 0 0-3 2',
  search: 'M21 21l-5-5 M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0',
  spark: 'M12 3l2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5z',
  plus: 'M12 5v14 M5 12h14',
  menu: 'M4 6h16 M4 12h16 M4 18h16',
  close: 'M6 6l12 12 M18 6L6 18',
  arrow: 'M5 12h14 M13 6l6 6-6 6',
  file: 'M14 2H5v20h14V7z M14 2v6h5 M8 13h8 M8 17h5',
  shield: 'M12 3l8 3v6c0 5-8 9-8 9s-8-4-8-9V6z M8 12l3 3 5-6',
  chat: 'M4 4h16v12H9l-5 4z M8 8h8 M8 12h5',
  chevron: 'M9 5l7 7-7 7',
}
</script>
<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.7"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path :d="paths[name] || paths.grid" />
  </svg>
</template>
