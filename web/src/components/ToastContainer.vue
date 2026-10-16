<template>
  <div class="fixed top-4 right-4 z-[9999] flex flex-col gap-2 w-80 max-w-[90vw]">
    <transition-group name="toast">
      <div
        v-for="t in state.toasts"
        :key="t.id"
        class="card px-4 py-3 shadow-lg border-l-4 flex gap-3 items-start"
        :class="borderClass(t.type)"
      >
        <span class="text-lg leading-none mt-0.5">{{ icon(t.type) }}</span>
        <div class="flex-1 min-w-0">
          <div class="text-sm font-semibold text-slate-800 dark:text-slate-100">{{ t.title }}</div>
          <div v-if="t.message" class="text-xs text-slate-500 dark:text-slate-400 mt-0.5 break-all">{{ t.message }}</div>
        </div>
        <button class="text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 text-xs" @click="close(t.id)">✕</button>
      </div>
    </transition-group>
  </div>
</template>

<script setup>
import { state } from '../store.js'

function close(id) {
  const i = state.toasts.findIndex((t) => t.id === id)
  if (i >= 0) state.toasts.splice(i, 1)
}
function icon(type) {
  return { success: '✅', error: '❌', warn: '⚠️', info: 'ℹ️' }[type] || 'ℹ️'
}
function borderClass(type) {
  return {
    success: 'border-l-emerald-500',
    error: 'border-l-rose-500',
    warn: 'border-l-amber-500',
    info: 'border-l-brand-500'
  }[type] || 'border-l-brand-500'
}
</script>

<style scoped>
.toast-enter-active,
.toast-leave-active {
  transition: all 0.3s ease;
}
.toast-enter-from {
  opacity: 0;
  transform: translateX(30px);
}
.toast-leave-to {
  opacity: 0;
  transform: translateX(30px);
}
</style>
