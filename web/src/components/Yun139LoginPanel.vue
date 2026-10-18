<template>
  <div class="y139-panel">
    <div class="y139-title">移动云盘登录（新的个人盘 · 账号密码回退）</div>
    <div class="y139-hint">
      填好上方「登录账号 / 登录密码」后点「账号密码登录」；若提示风控，先在浏览器登录
      <b>mail.10086.cn</b>，把完整 Cookie（含 <code>Os_SSo_Sid</code>、<code>RMKEY</code>）贴到「邮箱 Cookie」再点登录，
      会改用 Cookie 快速登录绕过邮箱密码校验。也可只填「授权令牌」后点「校验令牌」。
    </div>

    <div class="y139-controls">
      <button type="button" class="y139-btn" :disabled="busy !== ''" @click="doLogin">
        {{ busy === 'login' ? '登录中...' : '账号密码登录' }}
      </button>
      <button type="button" class="y139-btn-ghost" :disabled="busy !== ''" @click="doVerify">
        {{ busy === 'verify' ? '校验中...' : '校验令牌' }}
      </button>
      <button type="button" class="y139-btn-ghost" :disabled="busy !== ''" @click="doClear">
        {{ busy === 'clear' ? '清除中...' : '清除登录' }}
      </button>
    </div>

    <div class="y139-status" :class="st.logged_in ? 'ok' : 'idle'">{{ st.status }}</div>
    <div v-if="st.logged_in" class="y139-meta">
      <span>账号：{{ st.account || '—' }}</span>
      <span v-if="expiryText">令牌有效期至：{{ expiryText }}</span>
      <span v-if="st.has_password" class="y139-tag">已配置密码回退</span>
      <span v-if="st.has_cookies" class="y139-tag">已配置邮箱 Cookie</span>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api } from '../api'
import { toast, bootstrap } from '../store'

const busy = ref('')
const st = reactive({
  logged_in: false,
  status: '加载中...',
  account: '',
  expires_at: 0,
  has_password: false,
  has_cookies: false
})

const expiryText = computed(() => {
  if (!st.expires_at) return ''
  const d = new Date(st.expires_at)
  if (isNaN(d.getTime())) return ''
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
})

async function refreshStatus() {
  try {
    const res = await api.get('/api/drive/yun139/status')
    Object.assign(st, res)
  } catch (e) {
    st.status = e.message || '状态查询失败'
  }
}

async function doLogin() {
  busy.value = 'login'
  try {
    const res = await api.post('/api/drive/yun139/login')
    Object.assign(st, res)
    toast('success', '登录成功', `移动云盘账号：${res.account || '已登录'}`)
    await bootstrap()
  } catch (e) {
    toast('error', '登录失败', e.message)
    await refreshStatus()
  } finally {
    busy.value = ''
  }
}

async function doVerify() {
  busy.value = 'verify'
  try {
    const res = await api.post('/api/drive/yun139/verify')
    Object.assign(st, res)
    toast('success', '令牌有效', `移动云盘账号：${res.account || '已登录'}`)
    await bootstrap()
  } catch (e) {
    toast('error', '校验失败', e.message)
    await refreshStatus()
  } finally {
    busy.value = ''
  }
}

async function doClear() {
  busy.value = 'clear'
  try {
    const res = await api.post('/api/drive/yun139/clear')
    Object.assign(st, res)
    toast('success', '已清除登录', '移动云盘本地令牌已删除')
    await bootstrap()
  } catch (e) {
    toast('error', '清除失败', e.message)
  } finally {
    busy.value = ''
  }
}

onMounted(refreshStatus)
defineExpose({ refreshStatus })
</script>

<style scoped>
.y139-panel {
  margin-top: 14px;
  padding: 14px;
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 10px;
  background: rgba(148, 163, 184, 0.06);
}
.y139-title { font-size: 13px; font-weight: 600; margin-bottom: 6px; }
.y139-hint { font-size: 11px; line-height: 1.6; color: #94a3b8; margin-bottom: 10px; }
.y139-controls { display: flex; flex-wrap: wrap; gap: 8px; }
.y139-btn, .y139-btn-ghost {
  font-size: 12px;
  padding: 6px 12px;
  border-radius: 8px;
  border: 1px solid transparent;
  cursor: pointer;
}
.y139-btn { background: #0085d0; color: #fff; }
.y139-btn:disabled, .y139-btn-ghost:disabled { opacity: 0.6; cursor: not-allowed; }
.y139-btn-ghost { background: transparent; border-color: rgba(148, 163, 184, 0.4); color: inherit; }
.y139-status { margin-top: 10px; font-size: 12px; }
.y139-status.ok { color: #10b981; }
.y139-status.idle { color: #94a3b8; }
.y139-meta { margin-top: 8px; display: flex; flex-wrap: wrap; gap: 10px; font-size: 11px; color: #94a3b8; }
.y139-tag { padding: 1px 6px; border-radius: 6px; background: rgba(16, 185, 129, 0.12); color: #10b981; }
</style>
