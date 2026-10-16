<template>
  <div class="mb-3" :class="field.span === 2 ? 'col-span-2' : ''">
    <label class="label">
      {{ field.label }}
      <span v-if="field.hint" class="ml-1 text-slate-400 font-normal">（{{ field.hint }}）</span>
    </label>

    <!-- 开关 -->
    <label v-if="field.type === 'switch'" class="inline-flex items-center gap-2 cursor-pointer">
      <input
        type="checkbox"
        class="peer sr-only"
        :checked="!!modelValue"
        @change="emit('update:modelValue', $event.target.checked)"
      />
      <span class="relative w-10 h-5 bg-slate-300 dark:bg-slate-600 rounded-full peer-checked:bg-brand-500 transition-colors after:content-[''] after:absolute after:top-0.5 after:left-0.5 after:w-4 after:h-4 after:bg-white after:rounded-full after:transition-transform peer-checked:after:translate-x-5"></span>
      <span class="text-xs text-slate-500 dark:text-slate-400">{{ modelValue ? '已启用' : '已关闭' }}</span>
    </label>

    <!-- 下拉 -->
    <select v-else-if="field.type === 'select'" class="input" :value="modelValue" @change="emit('update:modelValue', $event.target.value)">
      <option v-for="opt in options" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
    </select>

    <!-- 多行 -->
    <textarea
      v-else-if="field.type === 'textarea'"
      class="input font-mono text-xs"
      rows="3"
      :placeholder="field.placeholder || ''"
      :value="modelValue ?? ''"
      @input="emit('update:modelValue', $event.target.value)"
    ></textarea>

    <!-- 数字 -->
    <input
      v-else-if="field.type === 'number'"
      type="number"
      class="input"
      :placeholder="field.placeholder || ''"
      :value="modelValue ?? ''"
      @input="emit('update:modelValue', Number($event.target.value))"
    />

    <!-- 密码 -->
    <input
      v-else-if="field.type === 'password'"
      type="password"
      class="input"
      :placeholder="field.placeholder || ''"
      :value="modelValue ?? ''"
      @input="emit('update:modelValue', $event.target.value)"
    />

    <!-- 标签组（逗号分隔） -->
    <input
      v-else-if="field.type === 'tags'"
      class="input"
      :placeholder="field.placeholder || '多个值用逗号分隔'"
      :value="modelValue ?? ''"
      @input="emit('update:modelValue', $event.target.value)"
    />

    <!-- 单行文本 -->
    <input
      v-else-if="field.type !== 'folder'"
      class="input"
      :placeholder="field.placeholder || ''"
      :value="modelValue ?? ''"
      @input="emit('update:modelValue', $event.target.value)"
    />

    <!-- 文件夹选择（文本输入 + 浏览按钮） -->
    <div v-else class="flex gap-2">
      <input
        class="input flex-1"
        :placeholder="field.placeholder || ''"
        :value="modelValue ?? ''"
        @input="emit('update:modelValue', $event.target.value)"
      />
      <button
        v-if="field.browseApi"
        type="button"
        class="btn btn-secondary whitespace-nowrap text-xs px-3"
        @click="emit('browse', field)"
      >
        浏览
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  modelValue: { default: '' },
  field: { type: Object, required: true }
})
const emit = defineEmits(['update:modelValue', 'browse'])

const options = computed(() => {
  const opts = props.field.options || []
  return opts.map((o) => (typeof o === 'string' ? { value: o, label: o } : o))
})
</script>
