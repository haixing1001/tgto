<template>
  <div class="p-6 max-w-5xl">
    <h2 class="text-lg font-bold mb-1">基础配置</h2>
    <p class="text-xs text-slate-400 mb-5">豆瓣分类梯队、反代服务器、REO 授权与观影资源登录</p>

    <!-- 豆瓣电影梯队与分类 -->
    <section class="card p-5 mb-4">
      <div class="flex items-center gap-2 mb-4">
        <span class="text-emerald-500">🎬</span>
        <h3 class="text-sm font-semibold">豆瓣电影梯队与分类</h3>
      </div>
      <div class="flex flex-wrap gap-2">
        <label
          v-for="tier in allTiers"
          :key="tier"
          class="inline-flex items-center gap-2 px-3 py-1.5 rounded-lg border cursor-pointer text-xs transition-colors"
          :class="form.douban_tiers.includes(tier) ? 'border-brand-500 bg-brand-500/10 text-brand-600 dark:text-brand-500' : 'border-slate-200 dark:border-slate-600 text-slate-500'"
        >
          <input type="checkbox" class="sr-only" :checked="form.douban_tiers.includes(tier)" @change="toggleTier(tier)" />
          {{ tier }}
        </label>
      </div>
    </section>

    <!-- 生效反代服务器 -->
    <section class="card p-5 mb-4">
      <div class="flex items-center gap-2 mb-4">
        <span class="text-sky-500">🔀</span>
        <h3 class="text-sm font-semibold">生效反代服务器</h3>
      </div>
      <input v-model="proxyNodesText" class="input text-xs" placeholder="多个节点用逗号分隔，如：hk1.example.com, sg2.example.com" />
      <div class="flex flex-wrap gap-2 mt-3">
        <span v-for="n in activeNodes" :key="n" class="badge bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-400">{{ n }}</span>
      </div>
    </section>

    <!-- REO Account 授权 -->
    <section class="card p-5 mb-4">
      <div class="flex items-center gap-2 mb-4">
        <span class="text-violet-500">🔐</span>
        <h3 class="text-sm font-semibold">REO Account 授权</h3>
      </div>
      <div class="flex flex-wrap items-end gap-3">
        <div class="flex-1 min-w-[220px]">
          <label class="label">REO 授权账号</label>
          <input v-model="form.reo_account" class="input" placeholder="填写授权账号" />
        </div>
        <button class="btn-ghost" @click="openReoPage">打开授权页</button>
        <button class="btn-ghost" @click="refreshReoStatus">刷新状态</button>
      </div>
      <div v-if="reoStatus" class="mt-3 text-xs" :class="reoStatus.ok ? 'text-emerald-500' : 'text-amber-500'">{{ reoStatus.text }}</div>
    </section>

    <!-- Guanying Session 观影登录 -->
    <section class="card p-5 mb-4">
      <div class="flex items-center gap-2 mb-4">
        <span class="text-rose-500">🎥</span>
        <h3 class="text-sm font-semibold">Guanying Session 观影登录</h3>
      </div>
      <label class="inline-flex items-center gap-2 cursor-pointer mb-4">
        <input v-model="form.guanying_enabled" type="checkbox" class="sr-only peer" />
        <span class="relative w-10 h-5 bg-slate-300 dark:bg-slate-600 rounded-full peer-checked:bg-brand-500 transition-colors after:content-[''] after:absolute after:top-0.5 after:left-0.5 after:w-4 after:h-4 after:bg-white after:rounded-full after:transition-transform peer-checked:after:translate-x-5"></span>
        <span class="text-xs text-slate-500 dark:text-slate-400">启用观影资源检索</span>
      </label>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div>
          <label class="label">观影账号</label>
          <input v-model="form.guanying_user" class="input" placeholder="观影平台账号" />
        </div>
        <div>
          <label class="label">观影密码</label>
          <input v-model="form.guanying_password" type="password" class="input" placeholder="观影平台密码" />
        </div>
      </div>
      <div class="mt-4 flex gap-2">
        <button class="btn-ghost" @click="guanyingLogin">登录观影</button>
        <button class="btn-ghost" @click="guanyingTest">测试登录态</button>
      </div>
    </section>

    <div class="flex justify-end">
      <button class="btn-primary px-6" :disabled="saving" @click="save">{{ saving ? '保存中...' : '保存基础配置' }}</button>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { state, saveConfig, toast } from '../store.js'

const allTiers = ['热门电影', '豆瓣电影Top250', '华语佳片', '冷门佳作', '独立电影', '纪录片佳作', '高分剧集']
const form = reactive({
  douban_tiers: [],
  proxy_nodes: [],
  reo_account: '',
  guanying_enabled: false,
  guanying_user: '',
  guanying_password: ''
})
const proxyNodesText = ref('')
const reoStatus = ref(null)
const saving = ref(false)

const activeNodes = computed(() =>
  proxyNodesText.value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
)

onMounted(() => {
  const cfg = state.config.basic_config || {}
  Object.assign(form, cfg)
  proxyNodesText.value = (cfg.proxy_nodes || []).join(', ')
})

function toggleTier(tier) {
  const i = form.douban_tiers.indexOf(tier)
  if (i >= 0) form.douban_tiers.splice(i, 1)
  else form.douban_tiers.push(tier)
}

async function save() {
  saving.value = true
  try {
    await saveConfig('basic_config', {
      ...form,
      proxy_nodes: activeNodes.value
    })
    toast('success', '保存成功', '基础配置已持久化')
  } catch (e) {
    toast('error', '保存失败', e.message)
  } finally {
    saving.value = false
  }
}

function openReoPage() {
  toast('info', '授权页', '已在沙盒模式记录授权请求（演示环境不打开外部页面）')
}

async function refreshReoStatus() {
  if (!form.reo_account.trim()) {
    toast('warn', '参数缺失', '请先填写 REO 授权账号')
    return
  }
  reoStatus.value = { ok: false, text: `账号 ${form.reo_account}：沙盒模式未连接授权服务器，配置凭证后可查询真实状态` }
  toast('info', '状态已刷新', '沙盒模式返回模拟状态')
}

function guanyingLogin() {
  if (!form.guanying_enabled) {
    toast('warn', '未启用', '请先勾选“启用观影资源检索”')
    return
  }
  if (!form.guanying_user.trim() || !form.guanying_password) {
    toast('warn', '参数缺失', '请填写观影账号与密码')
    return
  }
  toast('success', '登录请求已提交', `账号 ${form.guanying_user}（沙盒模式）`)
}

function guanyingTest() {
  if (!form.guanying_enabled) {
    toast('warn', '未启用', '请先勾选“启用观影资源检索”')
    return
  }
  toast(form.guanying_user ? 'success' : 'error', '登录态检测', form.guanying_user ? '会话有效（沙盒模式）' : '未检测到登录会话')
}
</script>
