<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-4">
      <div>
        <h2 class="text-lg font-bold">监控历史</h2>
        <p class="text-xs text-slate-400 mt-0.5">Telegram 频道监控产生的分享链接转存记录</p>
      </div>
      <button class="btn-ghost text-xs" @click="load">⟳ 刷新</button>
    </div>

    <!-- 网盘子标签 -->
    <div class="flex gap-1 border-b border-slate-200 dark:border-slate-700 mb-4">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        class="px-4 py-2 text-sm -mb-px border-b-2 transition-colors"
        :class="drive === tab.key ? 'border-brand-500 text-brand-600 dark:text-brand-500 font-medium' : 'border-transparent text-slate-500 hover:text-slate-700 dark:hover:text-slate-300'"
        @click="switchTab(tab.key)"
      >
        {{ tab.label }}
      </button>
    </div>

    <!-- 筛选与搜索 -->
    <div class="flex flex-wrap items-center gap-3 mb-4">
      <select v-model="status" class="input !w-36 !py-1.5 text-xs" @change="load">
        <option value="all">全部状态</option>
        <option value="成功">成功</option>
        <option value="跳过">跳过</option>
        <option value="失败">失败</option>
      </select>
      <input v-model="keyword" class="input !w-64 !py-1.5 text-xs" placeholder="搜索分享链接 / 频道 / 结果说明" @keyup.enter="load" />
      <button class="btn-ghost text-xs" @click="load">搜索</button>
    </div>

    <!-- 数据表格 -->
    <div class="card overflow-hidden">
      <table class="w-full text-xs">
        <thead class="bg-slate-50 dark:bg-slate-800/60 text-slate-500">
          <tr>
            <th class="text-left px-4 py-2.5 font-medium">时间</th>
            <th class="text-left px-4 py-2.5 font-medium">来源频道</th>
            <th class="text-left px-4 py-2.5 font-medium">分享链接</th>
            <th class="text-left px-4 py-2.5 font-medium">目标目录</th>
            <th class="text-left px-4 py-2.5 font-medium">状态</th>
            <th class="text-left px-4 py-2.5 font-medium">转存结果说明</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id" class="border-t border-slate-100 dark:border-slate-700/60 hover:bg-slate-50/60 dark:hover:bg-slate-800/40">
            <td class="px-4 py-2.5 text-slate-500 whitespace-nowrap">{{ fmtTime(row.created_at) }}</td>
            <td class="px-4 py-2.5">{{ row.channel || '-' }}</td>
            <td class="px-4 py-2.5 max-w-[220px]">
              <a :href="row.share_link" target="_blank" class="text-brand-500 hover:underline truncate block">{{ row.share_link }}</a>
            </td>
            <td class="px-4 py-2.5 text-slate-500">{{ row.target_dir || '-' }}</td>
            <td class="px-4 py-2.5"><span class="badge" :class="statusBadgeClass(row.status)">{{ row.status }}</span></td>
            <td class="px-4 py-2.5 text-slate-500">{{ row.message }}</td>
          </tr>
          <tr v-if="!rows.length">
            <td colspan="6" class="px-4 py-10 text-center text-slate-400">暂无记录</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 分页 -->
    <div class="flex items-center justify-between mt-4 text-xs text-slate-500">
      <span>共 {{ total }} 条</span>
      <div class="flex gap-2">
        <button class="btn-ghost text-xs" :disabled="page <= 1" @click="page-- && load()">上一页</button>
        <span class="px-2 py-1.5">第 {{ page }} 页</span>
        <button class="btn-ghost text-xs" :disabled="page * size >= total" @click="page++ && load()">下一页</button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { fmtTime, statusBadgeClass } from '../store.js'
import { api } from '../api.js'

const tabs = [
  { key: 'cloud139', label: '移动云盘' },
  { key: 'cloud123', label: '123 云盘' },
  { key: 'guangya', label: '光鸭云盘' },
  { key: 'tianyi', label: '天翼云盘' }
]
const drive = ref('cloud123')
const status = ref('all')
const keyword = ref('')
const page = ref(1)
const size = 10
const rows = ref([])
const total = ref(0)

function switchTab(key) {
  drive.value = key
  page.value = 1
  load()
}

async function load() {
  try {
    const data = await api.get(
      `/api/transfer/history?drive=${drive.value}&status=${status.value}&keyword=${encodeURIComponent(keyword.value)}&page=${page.value}&size=${size}`
    )
    rows.value = data.items || []
    total.value = data.total || 0
  } catch (e) {
    rows.value = []
    total.value = 0
  }
}

onMounted(load)
</script>
