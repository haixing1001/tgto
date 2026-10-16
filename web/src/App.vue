<template>
  <div class="h-full flex flex-col bg-slate-50 text-slate-800 dark:bg-[#0f1115] dark:text-slate-100">
    <ToastContainer />

    <!-- 登录页 -->
    <LoginView v-if="!state.token" />

    <!-- 后台主界面 -->
    <template v-else>
      <!-- 顶部首次运行提示横幅 -->
      <div
        v-if="!state.firstRunBannerClosed"
        class="bg-amber-50 dark:bg-amber-500/10 border-b border-amber-200 dark:border-amber-500/30 px-4 py-2 flex items-center gap-3 text-xs"
      >
        <span class="text-amber-500 text-base">💡</span>
        <span class="flex-1 text-amber-800 dark:text-amber-300">
          请先前往 <b>系统工具 - 全局设置</b> 完成基础设置，然后配置 123 云盘账号、115 Cookie、天翼云盘账号或光鸭云盘账号。
        </span>
        <button class="btn-primary" @click="navigate('drive-123-account')">去配置云盘</button>
        <button class="text-amber-500 hover:text-amber-700 px-1" @click="state.firstRunBannerClosed = true">✕</button>
      </div>

      <div class="flex-1 flex min-h-0">
        <!-- 左侧导航 -->
        <aside class="w-56 shrink-0 bg-white dark:bg-panel-dark border-r border-slate-200 dark:border-slate-700 flex flex-col">
          <div class="px-4 py-4 border-b border-slate-200 dark:border-slate-700">
            <div class="flex items-center gap-2">
              <div class="w-8 h-8 rounded-lg bg-brand-500 flex items-center justify-center text-white font-bold text-sm">T</div>
              <div>
                <div class="text-sm font-bold leading-tight">Tgto</div>
                <div class="text-[10px] text-slate-400">v{{ state.version }}</div>
              </div>
            </div>
            <button class="btn-primary w-full mt-3 text-xs" @click="quickModal = true">＋ 新建分享链接转存任务</button>
          </div>

          <nav class="flex-1 overflow-y-auto px-2 py-3 space-y-4">
            <div v-for="group in navGroups" :key="group.title">
              <div
                class="px-2 mb-1 text-[10px] font-semibold tracking-wider text-slate-400 uppercase flex items-center justify-between"
                :class="group.collapsible ? 'cursor-pointer select-none hover:text-slate-500' : ''"
                @click="toggleGroup(group)"
              >
                <span>{{ group.title }}</span>
                <span v-if="group.collapsible" class="text-[10px]">{{ group.expanded ? '▾' : '▸' }}</span>
              </div>
              <div v-show="!group.collapsible || group.expanded">
                <!-- 「云盘支持」：每个网盘是可展开的一级条目（就绪绿灯指示账号配置状态） -->
                <template v-if="group.driveGroups">
                  <div v-for="dg in group.driveGroups" :key="dg.driveKey" class="mb-0.5">
                    <div
                      class="nav-item"
                      :class="isDriveActive(dg) ? 'nav-item-active' : 'nav-item-idle'"
                      @click="toggleDrive(dg)"
                    >
                      <span>{{ dg.icon }} {{ dg.name }}</span>
                      <span
                        class="ml-auto w-2 h-2 rounded-full"
                        :class="driveReady(dg.driveKey, state.config) ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'"
                        :title="driveReady(dg.driveKey, state.config) ? '账号已配置' : '账号未配置'"
                      ></span>
                    </div>
                    <div v-show="expandedDrives[dg.driveKey]" class="pl-6">
                      <div
                        v-for="item in dg.items"
                        :key="item.key"
                        class="nav-item text-xs"
                        :class="state.currentView === item.key ? 'nav-item-active' : 'nav-item-idle'"
                        @click="navigate(item.key)"
                      >
                        <span>{{ item.label }}</span>
                      </div>
                    </div>
                  </div>
                </template>
                <!-- 普通一级条目 -->
                <template v-else>
                  <div
                    v-for="item in group.items"
                    :key="item.key"
                    class="nav-item"
                    :class="state.currentView === item.key ? 'nav-item-active' : 'nav-item-idle'"
                    @click="navigate(item.key)"
                  >
                    <span class="w-4 text-center">{{ item.icon }}</span>
                    <span>{{ item.label }}</span>
                  </div>
                </template>
              </div>
            </div>
          </nav>

          <div class="px-2 py-3 border-t border-slate-200 dark:border-slate-700 space-y-1">
            <div class="nav-item nav-item-idle" @click="toggleTheme()">
              <span class="w-4 text-center">{{ state.theme === 'dark' ? '🌞' : '🌙' }}</span>
              <span>{{ state.theme === 'dark' ? '切换浅色主题' : '切换深色主题' }}</span>
            </div>
            <div class="nav-item nav-item-idle" @click="logout()">
              <span class="w-4 text-center">🚪</span>
              <span>退出登录</span>
            </div>
            <div class="flex items-center gap-2 px-3 pt-1 text-[10px] text-slate-400">
              <span class="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></span>
              <span>在线 · {{ state.username }}</span>
            </div>
          </div>
        </aside>

        <!-- 主内容区 -->
        <main class="flex-1 min-w-0 overflow-y-auto">
          <component :is="currentViewComponent" :key="state.currentView" />
        </main>
      </div>
    </template>

    <!-- 全局快捷转存弹窗 -->
    <ModalDialog v-model="quickModal" title="新建分享链接转存任务" width="560px">
      <div class="space-y-3">
        <div>
          <label class="label">分享链接（支持 115 / 123云盘 / 光鸭云盘 / 天翼云盘）</label>
          <input v-model="quickForm.link" class="input" placeholder="https://123pan.com/s/xxxxxxx" />
        </div>
        <div>
          <label class="label">提取码（可选）</label>
          <input v-model="quickForm.code" class="input" placeholder="没有则留空" />
        </div>
        <div>
          <label class="label">目标目录（可选）</label>
          <input v-model="quickForm.target" class="input" placeholder="留空则使用该网盘默认目录" />
        </div>
        <div class="text-xs text-slate-400">提交后自动识别网盘类型并写入监控历史。</div>
      </div>
      <template #footer>
        <button class="btn-ghost" @click="quickModal = false">取消</button>
        <button class="btn-primary" :disabled="quickSubmitting" @click="submitQuickTransfer">
          {{ quickSubmitting ? '提交中...' : '立即转存' }}
        </button>
      </template>
    </ModalDialog>
  </div>
</template>

<script setup>
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { state, navigate, logout, toggleTheme, toast, bootstrap, initRouter } from './store.js'
import ToastContainer from './components/ToastContainer.vue'
import ModalDialog from './components/ModalDialog.vue'
import LoginView from './views/LoginView.vue'
import GlobalSettingsView from './views/GlobalSettingsView.vue'
import LogCenterView from './views/LogCenterView.vue'
import TransferHistoryView from './views/TransferHistoryView.vue'
import OrganizeHistoryView from './views/OrganizeHistoryView.vue'
import UtilityToolsView from './views/UtilityToolsView.vue'
import MediaDiscoveryView from './views/MediaDiscoveryView.vue'
import TrendingView from './views/TrendingView.vue'
import CalendarView from './views/CalendarView.vue'
import BasicConfigView from './views/BasicConfigView.vue'
import CloudDriveView from './views/CloudDriveView.vue'
import { api } from './api.js'
import { DRIVES, driveNavItems, driveGroups, driveReady, driveFromRoute } from './driveDefs.js'

// 左侧主导航分组。「云盘支持」分组由 driveDefs 生成：每个网盘是可展开条目，
// 其配置项子菜单为二级条目（子菜单已从视图内部左侧列上移到主导航）。
const navGroups = reactive([
  {
    title: '系统工具',
    items: [
      { key: 'global-settings', label: '全局设置', icon: '⚙️' },
      { key: 'log-center', label: '日志中心', icon: '📜' },
      { key: 'transfer-history', label: '监控历史', icon: '🕘' },
      { key: 'organize-history', label: '整理历史', icon: '🗂️' },
      { key: 'utility-tools', label: '实用工具', icon: '🧰' }
    ]
  },
  {
    title: '云盘支持',
    driveGroups: driveGroups()
  },
  {
    title: '影视发现',
    collapsible: true,
    expanded: false,
    items: [
      { key: 'media-discovery', label: '影视探索', icon: '🔍' },
      { key: 'trending', label: '榜单推荐', icon: '🔥' },
      { key: 'calendar', label: '追剧日历', icon: '📅' },
      { key: 'basic-config', label: '基础配置', icon: '🧩' }
    ]
  }
])

// 网盘配置页（所有 drive-* 路由共用 CloudDriveView，页内按路由渲染对应网盘+子菜单）
const driveNavKeys = driveNavItems().map((d) => d.route)

function componentForView(view) {
  const map = {
    'global-settings': GlobalSettingsView,
    'log-center': LogCenterView,
    'transfer-history': TransferHistoryView,
    'organize-history': OrganizeHistoryView,
    'utility-tools': UtilityToolsView,
    'media-discovery': MediaDiscoveryView,
    trending: TrendingView,
    calendar: CalendarView,
    'basic-config': BasicConfigView
  }
  if (map[view]) return map[view]
  if (driveNavKeys.includes(view)) return CloudDriveView
  return GlobalSettingsView
}

// 当前视图组件：由 state.currentView 派生，pushState / popstate 都能正确切换
const currentViewComponent = computed(() => componentForView(state.currentView))
const quickModal = ref(false)
const quickSubmitting = ref(false)
const quickForm = ref({ link: '', code: '', target: '' })

// 每个网盘条目的展开状态（响应式）
const expandedDrives = reactive({})

// 路由变化时：落在某网盘页面自动展开该网盘条目，落在「影视发现」分组自动展开该分组
function applyAutoExpand(view) {
  if (driveNavKeys.includes(view)) {
    const drive = driveFromRoute(view)
    if (drive) expandedDrives[drive] = true
  }
  const mediaGroup = navGroups.find((g) => g.title === '影视发现')
  if (mediaGroup && mediaGroup.items.some((item) => item.key === view)) {
    mediaGroup.expanded = true
  }
}

watch(() => state.currentView, (view) => applyAutoExpand(view))

// 网盘条目高亮：当前路由属于该网盘的任一子菜单
function isDriveActive(dg) {
  return dg.items.some((item) => item.key === state.currentView)
}

// 点击网盘条目：切换展开
function toggleDrive(dg) {
  expandedDrives[dg.driveKey] = !expandedDrives[dg.driveKey]
}

// 折叠分组的展开/收起切换
function toggleGroup(group) {
  if (group.collapsible) group.expanded = !group.expanded
}

onMounted(() => {
  initRouter(applyAutoExpand)
  applyAutoExpand(state.currentView)
  window.addEventListener('tgto:unauthorized', () => toast('warn', '登录状态', '会话已过期，请重新登录'))
})

onUnmounted(() => {
  window.removeEventListener('tgto:unauthorized', () => {})
})

async function submitQuickTransfer() {
  if (!quickForm.value.link.trim()) {
    toast('warn', '参数缺失', '请填写分享链接')
    return
  }
  quickSubmitting.value = true
  try {
    const row = await api.post('/api/transfer/submit', {
      link: quickForm.value.link.trim(),
      code: quickForm.value.code.trim(),
      target_dir: quickForm.value.target.trim()
    })
    toast('success', '转存任务已提交', `识别为 ${row.drive_name || row.drive} → ${row.target_dir}（${row.status}）`)
    quickModal.value = false
    quickForm.value = { link: '', code: '', target: '' }
    bootstrap()
  } catch (e) {
    toast('error', '提交失败', e.message)
  } finally {
    quickSubmitting.value = false
  }
}
</script>
