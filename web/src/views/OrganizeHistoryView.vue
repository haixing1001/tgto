<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-4">
      <div>
        <h2 class="text-lg font-bold">整理历史</h2>
        <p class="text-xs text-slate-400 mt-0.5">TMDB 影视 / MusicBrainz 无损音乐的识别、整理与重命名记录</p>
      </div>
      <div class="flex gap-2">
        <button class="btn-ghost text-xs" @click="load">⟳ 刷新</button>
        <button class="btn-primary text-xs" @click="runOrganize">{{ running ? '整理中...' : '▶ 立即整理' + currentDriveLabel }}</button>
        <button class="btn-ghost text-xs" @click="testModal = true">🧪 识别测试</button>
        <button class="btn-danger text-xs" @click="clearAll">🗑 清除历史</button>
      </div>
    </div>

    <!-- 7 个子标签页 -->
    <div class="flex flex-wrap gap-1 border-b border-slate-200 dark:border-slate-700 mb-4">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        class="px-3 py-2 text-xs -mb-px border-b-2 transition-colors"
        :class="activeTab === tab.key ? 'border-brand-500 text-brand-600 dark:text-brand-500 font-medium' : 'border-transparent text-slate-500 hover:text-slate-700 dark:hover:text-slate-300'"
        @click="switchTab(tab.key)"
      >
        {{ tab.label }}
      </button>
    </div>

    <!-- 筛选与搜索 -->
    <div class="flex flex-wrap items-center gap-3 mb-4">
      <select v-model="status" class="input !w-32 !py-1.5 text-xs" @change="load">
        <option value="all">全部状态</option>
        <option value="成功">成功</option>
        <option value="失败">失败</option>
        <option value="已回退">已回退</option>
      </select>
      <input v-model="keyword" class="input !w-72 !py-1.5 text-xs" placeholder="按原文件名 / 归档路径 / TMDB ID / 错误信息搜索" @keyup.enter="load" />
      <button class="btn-ghost text-xs" @click="load">搜索</button>
    </div>

    <!-- 批量操作工具栏 -->
    <div class="flex flex-wrap items-center gap-2 mb-3 text-xs">
      <button class="btn-ghost text-xs" @click="selectAll">☑ 全选当前页</button>
      <button class="btn-ghost text-xs" @click="selected = []">✕ 清空已选</button>
      <button class="btn-ghost text-xs" :disabled="!selected.length" @click="rollback">↩ 批量改名回退</button>
      <button class="btn-danger text-xs" :disabled="!selected.length" @click="removeSelected">🗑 批量删除文件</button>
      <span class="text-slate-400">已选 {{ selected.length }} 条</span>
    </div>

    <!-- 数据表格 -->
    <div class="card overflow-hidden">
      <table class="w-full text-xs">
        <thead class="bg-slate-50 dark:bg-slate-800/60 text-slate-500">
          <tr>
            <th class="w-8 px-3 py-2.5"></th>
            <th class="text-left px-3 py-2.5 font-medium">原文件名</th>
            <th class="text-left px-3 py-2.5 font-medium">归档路径 / 目标文件名</th>
            <th class="text-left px-3 py-2.5 font-medium">TMDB / MBID</th>
            <th class="text-left px-3 py-2.5 font-medium">状态</th>
            <th class="text-left px-3 py-2.5 font-medium">时间</th>
            <th class="text-left px-3 py-2.5 font-medium">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id" class="border-t border-slate-100 dark:border-slate-700/60 hover:bg-slate-50/60 dark:hover:bg-slate-800/40">
            <td class="px-3 py-2.5"><input type="checkbox" :checked="selected.includes(row.id)" @change="toggleSelect(row.id)" /></td>
            <td class="px-3 py-2.5 max-w-[200px]"><span class="truncate block" :title="row.original_name">{{ row.original_name }}</span></td>
            <td class="px-3 py-2.5 max-w-[280px]">
              <div class="truncate font-medium" :title="row.target_name">{{ row.target_name }}</div>
              <div class="truncate text-slate-400" :title="row.target_path">{{ row.target_path }}</div>
              <div v-if="row.error" class="text-rose-400 truncate" :title="row.error">{{ row.error }}</div>
            </td>
            <td class="px-3 py-2.5 text-slate-500">{{ row.tmdb_id || row.mbid || '-' }}</td>
            <td class="px-3 py-2.5"><span class="badge" :class="statusBadgeClass(row.status)">{{ row.status }}</span></td>
            <td class="px-3 py-2.5 text-slate-500 whitespace-nowrap">{{ fmtTime(row.created_at) }}</td>
            <td class="px-3 py-2.5">
              <button class="text-brand-500 hover:underline mr-2" @click="rollbackOne(row)">回退</button>
              <button class="text-rose-500 hover:underline" @click="removeOne(row)">删除</button>
            </td>
          </tr>
          <tr v-if="!rows.length">
            <td colspan="7" class="px-4 py-10 text-center text-slate-400">暂无记录</td>
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

    <!-- 文件名识别测试沙盒 -->
    <ModalDialog v-model="testModal" title="文件名识别测试沙盒" width="640px">
      <div class="space-y-3">
        <div>
          <label class="label">影视或无损音乐文件名</label>
          <input v-model="testFilename" class="input" placeholder="Shogun.2024.S01E05.2160p.DSNP.WEB-DL.DDP5.1.DV.HEVC-HHWEB.mkv" @keyup.enter="runTest" />
        </div>
        <div class="flex gap-2">
          <button class="btn-primary" :disabled="testing" @click="runTest">{{ testing ? '识别中...' : '开始识别' }}</button>
          <button class="btn-ghost" @click="fillSample('tv')">填入剧集示例</button>
          <button class="btn-ghost" @click="fillSample('movie')">填入电影示例</button>
          <button class="btn-ghost" @click="fillSample('music')">填入音乐示例</button>
        </div>
        <div v-if="result" class="card p-4 text-xs space-y-2 !shadow-none">
          <div class="grid grid-cols-2 gap-x-6 gap-y-1">
            <div><span class="text-slate-400">识别类型：</span><b>{{ typeLabel(result.type) }}</b></div>
            <div><span class="text-slate-400">匹配标题：</span><b>{{ result.title || '-' }}</b></div>
            <div><span class="text-slate-400">年份：</span>{{ result.year || '-' }}</div>
            <div><span class="text-slate-400">季 / 集：</span>{{ result.season ? 'S' + String(result.season).padStart(2, '0') : '-' }} {{ result.episode ? 'E' + String(result.episode).padStart(2, '0') : '' }}</div>
            <div><span class="text-slate-400">TMDB ID：</span>{{ result.tmdb_id || '待匹配' }}</div>
            <div><span class="text-slate-400">MBID：</span>{{ result.mbid || '待匹配' }}</div>
            <div><span class="text-slate-400">画质：</span>{{ result.resolution || '-' }} {{ result.source_tag || '' }}</div>
            <div><span class="text-slate-400">制作组：</span>{{ result.group || '-' }}</div>
          </div>
          <div class="pt-2 border-t border-slate-100 dark:border-slate-700">
            <div class="text-slate-400 mb-1">标准重命名结果：</div>
            <div class="font-mono text-brand-600 dark:text-brand-500">{{ result.target_name }}</div>
            <div class="text-slate-400 mt-2 mb-1">完整归档目录：</div>
            <div class="font-mono text-slate-500 break-all">{{ result.target_path }}</div>
          </div>
          <div v-if="result.notes && result.notes.length" class="pt-2 border-t border-slate-100 dark:border-slate-700 text-amber-500">
            <div v-for="(n, i) in result.notes" :key="i">· {{ n }}</div>
          </div>
        </div>
      </div>
    </ModalDialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { fmtTime, statusBadgeClass, toast } from '../store.js'
import { api } from '../api.js'
import ModalDialog from '../components/ModalDialog.vue'

// 7 个子分类 -> 后端查询参数
const tabs = [
  { key: 'cloud123', label: '123云盘', drive: 'cloud123', kind: '', music: false },
  { key: 'cloud123-music', label: '123云盘音乐', drive: 'cloud123', kind: 'music', music: true },
  { key: 'guangya', label: '光鸭云盘', drive: 'guangya', kind: '', music: false },
  { key: 'guangya-music', label: '光鸭云盘音乐', drive: 'guangya', kind: 'music', music: true },
  { key: 'cloud139', label: '移动云盘', drive: 'cloud139', kind: '', music: false },
  { key: 'cloud139-music', label: '移动云盘音乐', drive: 'cloud139', kind: 'music', music: true },
  { key: 'cloud139-share', label: '移动云盘分享', drive: 'cloud139', kind: 'share', music: false }
]

const activeTab = ref('cloud123')
const status = ref('all')
const keyword = ref('')
const page = ref(1)
const size = 10
const rows = ref([])
const total = ref(0)
const selected = ref([])
const running = ref(false)
const testModal = ref(false)
const testFilename = ref('')
const testing = ref(false)
const result = ref(null)

const currentTab = computed(() => tabs.find((t) => t.key === activeTab.value) || tabs[0])
const currentDriveLabel = computed(() => currentTab.value.label)

function switchTab(key) {
  activeTab.value = key
  page.value = 1
  selected.value = []
  load()
}

async function load() {
  try {
    const data = await api.get(
      `/api/organize/history?drive=${currentTab.value.drive}&kind=${currentTab.value.kind}&status=${status.value}&keyword=${encodeURIComponent(keyword.value)}&page=${page.value}&size=${size}`
    )
    let items = data.items || []
    if (!currentTab.value.music && !currentTab.value.kind) {
      items = items.filter((r) => r.kind !== 'music')
    }
    rows.value = items
    total.value = data.total || 0
  } catch (e) {
    rows.value = []
    total.value = 0
  }
}

function toggleSelect(id) {
  const i = selected.value.indexOf(id)
  if (i >= 0) selected.value.splice(i, 1)
  else selected.value.push(id)
}
function selectAll() {
  selected.value = rows.value.map((r) => r.id)
}

async function runOrganize() {
  running.value = true
  try {
    const res = await api.post('/api/organize/run', { drive: currentTab.value.drive })
    toast('success', '整理完成', `成功 ${res.success} 条，失败 ${res.failed} 条`)
    await load()
  } catch (e) {
    toast('error', '整理失败', e.message)
  } finally {
    running.value = false
  }
}

async function rollback() {
  if (!selected.value.length) return
  try {
    const res = await api.post('/api/organize/rollback', { ids: selected.value })
    toast('success', '回退完成', `${res.affected} 条记录已回退为原始文件名`)
    selected.value = []
    await load()
  } catch (e) {
    toast('error', '回退失败', e.message)
  }
}
async function rollbackOne(row) {
  try {
    await api.post('/api/organize/rollback', { ids: [row.id] })
    toast('success', '已回退', row.target_name)
    await load()
  } catch (e) {
    toast('error', '回退失败', e.message)
  }
}
async function removeSelected() {
  if (!selected.value.length) return
  try {
    const res = await api.post('/api/organize/delete', { ids: selected.value })
    toast('success', '删除完成', `${res.affected} 条记录已删除`)
    selected.value = []
    await load()
  } catch (e) {
    toast('error', '删除失败', e.message)
  }
}
async function removeOne(row) {
  try {
    await api.post('/api/organize/delete', { ids: [row.id] })
    await load()
  } catch (e) {
    toast('error', '删除失败', e.message)
  }
}
async function clearAll() {
  try {
    const res = await api.post('/api/organize/clear')
    toast('success', '已清空', `${res.affected} 条整理历史已清除`)
    selected.value = []
    await load()
  } catch (e) {
    toast('error', '操作失败', e.message)
  }
}

function fillSample(kind) {
  testFilename.value = {
    tv: 'Shogun.2024.S01E05.2160p.DSNP.WEB-DL.DDP5.1.DV.HEVC-HHWEB.mkv',
    movie: '沙丘2.2024.2160p.UHD.BluRay.REMUX.HDR.HEVC.TrueHD.7.1.Atmos-NBGY.mkv',
    music: 'Jay.Chou.-.Fantasy.2001.FLAC/02.简单爱.flac'
  }[kind]
  runTest()
}

function typeLabel(t) {
  return { movie: '电影', tv: '剧集', music: '音乐', unknown: '未识别' }[t] || t
}

async function runTest() {
  if (!testFilename.value.trim()) {
    toast('warn', '参数缺失', '请输入文件名')
    return
  }
  testing.value = true
  try {
    result.value = await api.post('/api/organize/test', { filename: testFilename.value.trim() })
  } catch (e) {
    toast('error', '识别失败', e.message)
  } finally {
    testing.value = false
  }
}

onMounted(load)
</script>
