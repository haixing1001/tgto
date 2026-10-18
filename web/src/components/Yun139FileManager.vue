<template>
  <div class="yfm">
    <div class="yfm-toolbar">
      <div class="flex items-center gap-1 text-xs min-w-0">
        <button class="yfm-crumb" :disabled="fm.pathStack.length <= 1" @click="goUp">⬆ 上级</button>
        <span v-for="(c, i) in fm.pathStack" :key="i" class="flex items-center gap-1 min-w-0">
          <span class="text-slate-300 dark:text-slate-600">/</span>
          <button class="yfm-crumb truncate" @click="jump(i)">{{ c.name }}</button>
        </span>
      </div>
      <div class="flex-1"></div>
      <button class="yfm-btn" @click="openMkdir">新建文件夹</button>
      <button class="yfm-btn-stop" @click="load">{{ fm.loading ? '刷新中...' : '刷新' }}</button>
    </div>

    <div v-if="fm.error" class="yfm-error">{{ fm.error }}</div>

    <div class="yfm-table-wrap">
      <table class="yfm-table">
        <thead>
          <tr>
            <th class="w-8"><input type="checkbox" :checked="allChecked" @change="toggleAll($event.target.checked)" /></th>
            <th>名称</th>
            <th class="w-24">大小</th>
            <th class="w-40">修改时间</th>
            <th class="w-44">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="fm.loading"><td colspan="5" class="yfm-empty">加载中...</td></tr>
          <tr v-else-if="!fm.items.length"><td colspan="5" class="yfm-empty">暂无文件</td></tr>
          <tr v-for="it in fm.items" :key="it.id">
            <td><input type="checkbox" :value="it.id" v-model="fm.checked" /></td>
            <td>
              <button v-if="it.isDir" class="yfm-name yfm-dir" @click="enter(it)">📁 {{ it.name }}</button>
              <span v-else class="yfm-name">📄 {{ it.name }}</span>
            </td>
            <td class="text-slate-400">{{ it.isDir ? '—' : sizeText(it.size) }}</td>
            <td class="text-slate-400">{{ timeText(it.updatedAt) }}</td>
            <td class="yfm-ops">
              <button class="yfm-op" @click="openRename(it)">重命名</button>
              <button class="yfm-op" @click="openBatch('move', it)">移动</button>
              <button class="yfm-op" @click="openBatch('copy', it)">复制</button>
              <button v-if="!it.isDir" class="yfm-op" @click="download(it)">下载</button>
              <button class="yfm-op yfm-op-danger" @click="openDelete(it)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="yfm-foot">
      <span v-if="fm.checked.length" class="text-brand-600 dark:text-brand-500 font-medium">已选 {{ fm.checked.length }} 项</span>
      <span v-else class="text-slate-400">共 {{ fm.items.length }} 项</span>
      <div class="flex-1"></div>
      <button v-if="fm.checked.length" class="text-xs yfm-btn-danger" @click="openBulkDelete">一键删除所选 {{ fm.checked.length }} 项</button>
      <button v-else class="yfm-btn-stop" disabled>一键删除所选</button>
      <button class="yfm-btn-stop" @click="openClearRecycle">清空回收站</button>
    </div>

    <ModalDialog v-model="fm.dialog" :title="fm.dialogTitle" width="max-w-md">
      <div v-if="fm.dialogKind === 'mkdir' || fm.dialogKind === 'rename'" class="space-y-3">
        <div>
          <label class="label">{{ fm.dialogKind === 'mkdir' ? '文件夹名称' : '新名称' }}</label>
          <input v-model="fm.dialogInput" class="input text-sm" :placeholder="fm.dialogKind === 'mkdir' ? '请输入文件夹名称' : '请输入新名称'" @keyup.enter="confirm" />
        </div>
      </div>
      <div v-else-if="fm.dialogKind === 'move' || fm.dialogKind === 'copy'" class="space-y-3">
        <div>
          <label class="label">目标目录 ID</label>
          <input v-model="fm.dialogInput" class="input text-sm" placeholder="留空表示根目录 /" />
          <p class="mt-1 text-xs text-slate-400">可在目标目录里点「复制当前目录 ID」取得，或填 / 表示根目录。</p>
          <button class="mt-1 text-xs text-brand-600" @click="copyCurrentID">复制当前目录 ID：{{ currentParent() }}</button>
        </div>
      </div>
      <div v-else-if="fm.dialogKind === 'delete'" class="text-sm text-slate-600 dark:text-slate-300">
        <p>确定删除所选 {{ fm.checked.length }} 项吗？文件将移入回收站。</p>
        <ul v-if="checkedNames.length" class="mt-2 max-h-32 overflow-y-auto space-y-0.5 text-xs text-slate-500 dark:text-slate-400">
          <li v-for="(n, i) in checkedNames.slice(0, 5)" :key="i" class="truncate">{{ n }}</li>
          <li v-if="checkedNames.length > 5" class="text-slate-400">……等共 {{ checkedNames.length }} 项</li>
        </ul>
      </div>
      <div v-else-if="fm.dialogKind === 'clearRecycle'" class="text-sm text-slate-600 dark:text-slate-300">
        <p>确定清空移动云盘回收站吗？回收站内的文件将被永久删除，且无法恢复。</p>
      </div>
      <template #footer>
        <button class="btn-ghost text-xs" @click="fm.dialog = false">取消</button>
        <button
          class="text-xs"
          :class="fm.dialogKind === 'delete' || fm.dialogKind === 'clearRecycle' ? 'btn-danger' : 'btn-primary'"
          :disabled="fm.dialogBusy"
          @click="confirm"
        >
          {{ fm.dialogBusy ? '处理中...' : '确定' }}
        </button>
      </template>
    </ModalDialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive } from 'vue'
import { toast } from '../store.js'
import ModalDialog from './ModalDialog.vue'
import { api } from '../api.js'

const ROOT_ID = '/'

const fm = reactive({
  loading: false,
  error: '',
  items: [],
  checked: [],
  pathStack: [{ id: ROOT_ID, name: '根目录' }],
  dialog: false,
  dialogKind: '',
  dialogTitle: '',
  dialogInput: '',
  dialogBusy: false,
  dialogTarget: null
})

const allChecked = computed(() => fm.items.length > 0 && fm.checked.length === fm.items.length)
const checkedItems = computed(() => fm.items.filter((it) => fm.checked.includes(it.id)))
const checkedNames = computed(() => checkedItems.value.map((it) => (it.isDir ? '📁 ' : '📄 ') + it.name))

function currentParent() {
  return fm.pathStack[fm.pathStack.length - 1].id
}

function normalize(res) {
  const list = res.items || res.files || []
  return list.map((it) => ({
    id: it.id,
    name: it.name,
    isDir: it.isDir ?? it.is_dir ?? false,
    size: Number(it.size || 0),
    updatedAt: it.updatedAt || it.updated_at || ''
  }))
}

async function load() {
  fm.loading = true
  fm.error = ''
  fm.checked = []
  try {
    const res = await api.get(`/api/drive/yun139/files?parent_id=${encodeURIComponent(currentParent())}&page_size=500`)
    fm.items = normalize(res)
  } catch (e) {
    fm.error = e.message || '加载失败'
    fm.items = []
  } finally {
    fm.loading = false
  }
}

function enter(it) {
  fm.pathStack.push({ id: it.id, name: it.name })
  load()
}

function goUp() {
  if (fm.pathStack.length > 1) {
    fm.pathStack.pop()
    load()
  }
}

function jump(i) {
  if (i >= 0 && i < fm.pathStack.length - 1) {
    fm.pathStack.splice(i + 1)
    load()
  }
}

function toggleAll(on) {
  fm.checked = on ? fm.items.map((it) => it.id) : []
}

function sizeText(n) {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`
}

function timeText(s) {
  if (!s) return '—'
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return s
  return d.toLocaleString('zh-CN', { hour12: false })
}

function openMkdir() {
  fm.dialogKind = 'mkdir'
  fm.dialogTitle = '新建文件夹'
  fm.dialogInput = ''
  fm.dialog = true
}

function openRename(it) {
  fm.dialogKind = 'rename'
  fm.dialogTitle = '重命名'
  fm.dialogInput = it.name
  fm.dialogTarget = it
  fm.dialog = true
}

function openBatch(kind, it) {
  fm.dialogKind = kind
  fm.dialogTitle = kind === 'move' ? '移动到…' : '复制到…'
  fm.dialogInput = ''
  fm.dialogTarget = it
  fm.dialog = true
}

function openDelete(it) {
  fm.checked = [it.id]
  fm.dialogKind = 'delete'
  fm.dialogTitle = '删除确认'
  fm.dialog = true
}

function openBulkDelete() {
  fm.dialogKind = 'delete'
  fm.dialogTitle = '删除确认'
  fm.dialog = true
}

function openClearRecycle() {
  fm.dialogKind = 'clearRecycle'
  fm.dialogTitle = '清空回收站'
  fm.dialog = true
}

function copyCurrentID() {
  const id = currentParent()
  if (navigator.clipboard?.writeText) {
    navigator.clipboard.writeText(id).then(
      () => toast('success', '已复制', id),
      () => toast('info', '当前目录 ID', id)
    )
  } else {
    toast('info', '当前目录 ID', id)
  }
}

async function confirm() {
  const kind = fm.dialogKind
  fm.dialogBusy = true
  try {
    if (kind === 'mkdir') {
      const name = fm.dialogInput.trim()
      if (!name) {
        toast('error', '名称不能为空')
        return
      }
      await api.post('/api/drive/yun139/files/mkdir', { dir_name: name, parent_id: currentParent() })
      toast('success', '已创建', name)
    } else if (kind === 'rename') {
      const name = fm.dialogInput.trim()
      if (!name) {
        toast('error', '名称不能为空')
        return
      }
      await api.post('/api/drive/yun139/files/rename', { file_id: fm.dialogTarget.id, new_name: name })
      toast('success', '已重命名', name)
    } else if (kind === 'move' || kind === 'copy') {
      const target = fm.dialogInput.trim() || ROOT_ID
      const ids = fm.checked.length ? fm.checked : [fm.dialogTarget.id]
      await api.post(`/api/drive/yun139/files/${kind}`, {
        items: ids.map((id) => ({ fileId: id })),
        target_folder_id: target
      })
      toast('success', kind === 'move' ? '已移动' : '已复制', `目标目录 ${target}`)
    } else if (kind === 'delete') {
      await api.post('/api/drive/yun139/files/delete', { items: fm.checked.map((id) => ({ fileId: id })) })
      toast('success', '已删除', '文件已移入回收站')
    } else if (kind === 'clearRecycle') {
      await api.post('/api/drive/yun139/files/recycle/clear', {})
      toast('success', '已清空回收站')
    }
    fm.dialog = false
    fm.dialogTarget = null
    await load()
  } catch (e) {
    toast('error', '操作失败', e.message)
  } finally {
    fm.dialogBusy = false
  }
}

async function download(it) {
  try {
    const res = await api.post('/api/drive/yun139/files/download_url', { file_id: it.id })
    if (res.url) {
      window.open(res.url, '_blank', 'noopener')
    } else {
      toast('error', '未获取到下载地址')
    }
  } catch (e) {
    toast('error', '获取下载地址失败', e.message)
  }
}

onMounted(load)
</script>

<style scoped>
.yfm {
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 12px;
  padding: 12px;
  background: rgba(148, 163, 184, 0.04);
}
.yfm-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.yfm-crumb {
  font-size: 12px;
  color: #64748b;
  padding: 2px 6px;
  border-radius: 6px;
  max-width: 180px;
}
.yfm-crumb:hover:not(:disabled) {
  background: rgba(148, 163, 184, 0.16);
  color: #0f172a;
}
.yfm-crumb:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.yfm-btn {
  font-size: 12px;
  padding: 4px 10px;
  border-radius: 8px;
  background: #0085d0;
  color: #fff;
}
.yfm-btn-stop {
  font-size: 12px;
  padding: 4px 10px;
  border-radius: 8px;
  border: 1px solid rgba(148, 163, 184, 0.4);
  color: #475569;
}
.yfm-btn-stop:disabled {
  opacity: 0.45;
}
.yfm-btn-danger {
  font-size: 12px;
  padding: 4px 10px;
  border-radius: 8px;
  background: rgba(239, 68, 68, 0.12);
  color: #dc2626;
}
.yfm-error {
  margin-bottom: 8px;
  padding: 6px 10px;
  border-radius: 8px;
  background: rgba(239, 68, 68, 0.1);
  color: #dc2626;
  font-size: 12px;
}
.yfm-table-wrap {
  overflow-x: auto;
}
.yfm-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.yfm-table th {
  text-align: left;
  font-weight: 500;
  color: #94a3b8;
  padding: 6px 8px;
  border-bottom: 1px solid rgba(148, 163, 184, 0.2);
}
.yfm-table td {
  padding: 6px 8px;
  border-bottom: 1px solid rgba(148, 163, 184, 0.12);
}
.yfm-empty {
  text-align: center;
  color: #94a3b8;
  padding: 16px 0;
}
.yfm-name {
  font-size: 12px;
  color: #334155;
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: inline-block;
  vertical-align: middle;
}
.yfm-dir {
  color: #0085d0;
  font-weight: 500;
}
.yfm-ops {
  white-space: nowrap;
}
.yfm-op {
  font-size: 11px;
  color: #475569;
  padding: 2px 6px;
  border-radius: 6px;
  margin-right: 2px;
}
.yfm-op:hover {
  background: rgba(148, 163, 184, 0.16);
}
.yfm-op-danger {
  color: #dc2626;
}
.yfm-foot {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
  font-size: 12px;
}
</style>
