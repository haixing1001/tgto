<template>
  <div class="ty-qr-panel">
    <div class="ty-qr-panel-title">天翼云盘扫码登录</div>
    <div class="ty-qr-hint">无需填写账号或密码即可扫码登录。扫码成功后会自动更新天翼云盘登录 Cookie。</div>

    <div class="ty-qr-controls">
      <button type="button" class="ty-qr-btn" :disabled="busy !== ''" @click="start">
        {{ busy === 'start' ? '生成中...' : '生成二维码' }}
      </button>
      <button type="button" class="ty-qr-btn-stop" :disabled="!session.uuid" @click="stop">
        停止扫码
      </button>
      <button type="button" class="ty-qr-btn-stop" :disabled="busy === 'clear'" @click="clearLogin">
        {{ busy === 'clear' ? '清除中...' : '清除登录' }}
      </button>
    </div>

    <div v-if="session.uuid" class="ty-qr-body">
      <div class="ty-qr-canvas">
        <img v-if="qrDataUrl" :src="qrDataUrl" alt="天翼云盘扫码二维码" class="w-full h-full" />
        <span v-else class="text-[11px] text-slate-400">二维码生成中...</span>
      </div>
      <div class="ty-qr-meta">
        <div>状态：{{ statusText }}</div>
        <div v-if="countdown > 0">剩余有效期：<span class="ty-qr-countdown">{{ countdown }}s</span></div>
        <div class="text-[10px] break-all text-slate-400">{{ session.qr_url }}</div>
      </div>
    </div>

    <div class="ty-qr-status" :class="statusClass">{{ statusMessage }}</div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import QRCode from 'qrcode'
import { api } from '../api'
import { toast } from '../store'

// ---------------- 状态 ----------------

const busy = ref('')
const session = reactive({ uuid: '', qr_url: '', expires_in: 120, status: '', message: '' })
const tyStatus = reactive({ logged_in: false, status: '', account: '', in_progress: false })
const pollState = reactive({ status: '', message: '' })
const countdown = ref(0)
const qrDataUrl = ref('')
const loginDone = ref(false)

let pollTimer = null
let countdownTimer = null

const statusText = computed(() => {
  const map = {
    waiting: '等待扫描',
    scanned: '已扫描，待确认',
    success: '登录成功',
    expired: '已过期',
    failed: '失败',
    error: '出错',
    cancelled: '已取消'
  }
  return map[pollState.status] || '等待生成'
})

const statusMessage = computed(() => {
  if (loginDone.value) return `✅ 扫码登录成功${tyStatus.account ? `，账号：${tyStatus.account}` : ''}，天翼云盘会话已保存`
  if (pollState.message) return pollState.message
  if (tyStatus.logged_in) return `已登录${tyStatus.account ? `，账号：${tyStatus.account}` : ''}`
  return tyStatus.status || '未登录'
})

const statusClass = computed(() => {
  if (loginDone.value) return 'ok'
  if (pollState.status === 'scanned') return 'warn'
  if (pollState.status === 'failed' || pollState.status === 'error') return 'err'
  return ''
})

// ---------------- 轮询 ----------------

async function refreshStatus() {
  try {
    const res = await api.get('/api/drive/tianyi/status')
    tyStatus.logged_in = !!res.logged_in
    tyStatus.status = res.status || ''
    tyStatus.account = res.account || ''
    tyStatus.in_progress = !!res.in_progress
  } catch { /* 状态查询失败不打扰面板 */ }
}

async function poll() {
  if (!session.uuid) return
  try {
    const res = await api.post('/api/drive/tianyi/qr/status')
    pollState.status = res.status || ''
    pollState.message = res.message || ''
    if (res.status === 'success') {
      loginDone.value = true
      stopPoll()
      stopCountdown()
      await refreshStatus()
    } else if (res.status === 'expired' || res.status === 'failed' || res.status === 'error' || res.status === 'idle') {
      stopPoll()
      stopCountdown()
      session.uuid = ''
      qrDataUrl.value = ''
    }
  } catch (e) {
    pollState.status = 'error'
    pollState.message = e.message || '轮询失败'
  }
}

function startPoll() {
  stopPoll()
  pollTimer = setInterval(poll, 2000)
}

function stopPoll() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
}

function startCountdown() {
  stopCountdown()
  countdown.value = session.expires_in || 120
  countdownTimer = setInterval(() => {
    countdown.value -= 1
    if (countdown.value <= 0) {
      countdown.value = 0
      stopCountdown()
      stopPoll()
      session.uuid = ''
      qrDataUrl.value = ''
      pollState.status = 'expired'
      pollState.message = '二维码已过期，请重新生成'
    }
  }, 1000)
}

function stopCountdown() {
  if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
}

// ---------------- 动作 ----------------

async function start() {
  busy.value = 'start'
  loginDone.value = false
  try {
    const res = await api.post('/api/drive/tianyi/qr/start')
    session.uuid = res.uuid || ''
    session.qr_url = res.qr_url || ''
    session.expires_in = res.expires_in || 120
    session.status = res.status || ''
    session.message = res.message || ''
    pollState.status = 'waiting'
    pollState.message = '请使用天翼云盘 APP 扫描二维码'
    // 二维码内容为完整扫码 URL（天翼 APP 识别后完成登录确认）
    qrDataUrl.value = session.qr_url
      ? await QRCode.toDataURL(session.qr_url, { width: 200, margin: 1, color: { dark: '#1e293b', light: '#ffffff' } })
      : ''
    startPoll()
    startCountdown()
  } catch (e) {
    toast('error', '生成二维码失败', e.message)
  } finally {
    busy.value = ''
  }
}

async function stop() {
  try { await api.post('/api/drive/tianyi/qr/cancel') } catch { /* 取消失败不打扰 */ }
  stopPoll()
  stopCountdown()
  session.uuid = ''
  qrDataUrl.value = ''
  pollState.status = 'cancelled'
  pollState.message = '已取消扫码'
}

async function clearLogin() {
  busy.value = 'clear'
  try {
    await api.post('/api/drive/tianyi/login/clear')
    toast('success', '已清除登录', '天翼云盘登录信息已清除')
    loginDone.value = false
    pollState.status = ''
    pollState.message = ''
    await refreshStatus()
  } catch (e) {
    toast('error', '清除失败', e.message)
  } finally {
    busy.value = ''
  }
}

onMounted(() => refreshStatus())
onUnmounted(() => { stopPoll(); stopCountdown() })

// 暴露刷新方法供父组件在配置保存后调用
defineExpose({ refreshStatus })
</script>
