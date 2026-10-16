<template>
  <teleport to="body">
    <div v-if="modelValue" class="fixed inset-0 z-[9998] flex items-center justify-center p-4">
      <div class="absolute inset-0 bg-black/50 backdrop-blur-sm" @click="onBackdrop"></div>
      <div class="relative card w-full shadow-2xl overflow-hidden" :style="{ maxWidth: width }">
        <div class="flex items-center justify-between px-5 py-3 border-b border-slate-200 dark:border-slate-700">
          <h3 class="text-sm font-semibold text-slate-800 dark:text-slate-100">{{ title }}</h3>
          <button class="text-slate-400 hover:text-slate-600 dark:hover:text-slate-200" @click="close">✕</button>
        </div>
        <div class="px-5 py-4 max-h-[70vh] overflow-y-auto">
          <slot />
        </div>
        <div v-if="$slots.footer" class="px-5 py-3 border-t border-slate-200 dark:border-slate-700 flex justify-end gap-2">
          <slot name="footer" />
        </div>
      </div>
    </div>
  </teleport>
</template>

<script setup>
import { watch } from 'vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  title: { type: String, default: '' },
  width: { type: String, default: '520px' },
  closeOnBackdrop: { type: Boolean, default: true }
})
const emit = defineEmits(['update:modelValue'])

function close() {
  emit('update:modelValue', false)
}
function onBackdrop() {
  if (props.closeOnBackdrop) close()
}
watch(
  () => props.modelValue,
  (v) => {
    document.body.style.overflow = v ? 'hidden' : ''
  }
)
</script>
