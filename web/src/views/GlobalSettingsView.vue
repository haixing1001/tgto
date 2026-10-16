<template>
  <div class="p-6 max-w-5xl">
    <h2 class="text-lg font-bold mb-1">全局设置</h2>
    <p class="text-xs text-slate-400 mb-5">网络代理、Telegram Bot 与频道监控轮询的基础配置</p>

    <!-- 网络代理卡片 -->
    <section class="card p-5 mb-4">
      <div class="flex items-center gap-2 mb-4">
        <span class="text-brand-500">🌐</span>
        <h3 class="text-sm font-semibold">网络代理</h3>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div>
          <label class="label">代理地址（单地址模式）</label>
          <input v-model="form.proxy" class="input" placeholder="http://127.0.0.1:7890" />
        </div>
        <div>
          <label class="label">TMDB API Key</label>
          <input v-model="form.tmdb_api_key" class="input" placeholder="配置后影视探索切换为 TMDB 真实数据" />
        </div>
      </div>
      <div class="mt-4 flex gap-2">
        <button class="btn-ghost" :disabled="testingProxy" @click="testProxy">{{ testingProxy ? '测试中...' : '测试代理' }}</button>
        <button class="btn-ghost" :disabled="testingKey" @click="testTmdb">{{ testingKey ? '测试中...' : '测试 TMDB Key' }}</button>
      </div>
    </section>

    <!-- Telegram Bot 配置卡片 -->
    <section class="card p-5 mb-4">
      <div class="flex items-center gap-2 mb-4">
        <span class="text-sky-500">✈️</span>
        <h3 class="text-sm font-semibold">Telegram Bot 配置</h3>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div>
          <label class="label">主机器人 Bot Token</label>
          <input v-model="form.tg_bot_token" class="input" placeholder="123456:ABC-DEF..." />
        </div>
        <div>
          <label class="label">你的 TG 用户 ID</label>
          <input v-model="form.tg_user_id" class="input" placeholder="123456789" />
        </div>
      </div>
      <div class="mt-4">
        <button class="btn-ghost" :disabled="testingTg" @click="testTelegram">{{ testingTg ? '发送中...' : '发送测试消息' }}</button>
      </div>
    </section>

    <!-- 频道监控轮询卡片 -->
    <section class="card p-5 mb-4">
      <div class="flex items-center gap-2 mb-4">
        <span class="text-amber-500">⏱️</span>
        <h3 class="text-sm font-semibold">频道监控轮询</h3>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div>
          <label class="label">频道监控轮询节奏（分钟）</label>
          <input v-model.number="form.monitor_interval" type="number" min="1" class="input" />
        </div>
        <div>
          <label class="label">刮削元数据首选语言</label>
          <select v-model="form.metadata_language" class="input">
            <option value="zh-CN">简体中文（zh-CN）</option>
            <option value="zh-TW">繁体中文（zh-TW）</option>
            <option value="en-US">English（en-US）</option>
            <option value="ja-JP">日本語（ja-JP）</option>
          </select>
        </div>
      </div>
    </section>

    <div class="flex justify-end">
      <button class="btn-primary px-6" :disabled="saving" @click="save">{{ saving ? '保存中...' : '保存全局设置' }}</button>
    </div>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { state, saveConfig, toast } from '../store.js'
import { api } from '../api.js'

const form = reactive({
  proxy: '',
  tmdb_api_key: '',
  tg_bot_token: '',
  tg_user_id: '',
  monitor_interval: 5,
  metadata_language: 'zh-CN'
})
const saving = ref(false)
const testingProxy = ref(false)
const testingTg = ref(false)
const testingKey = ref(false)

onMounted(() => {
  const cfg = state.config.global_settings || {}
  Object.assign(form, cfg)
})

async function save() {
  saving.value = true
  try {
    await saveConfig('global_settings', { ...form })
    toast('success', '保存成功', '全局设置已持久化')
  } catch (e) {
    toast('error', '保存失败', e.message)
  } finally {
    saving.value = false
  }
}

async function testProxy() {
  if (!form.proxy.trim()) {
    toast('warn', '参数缺失', '请先填写代理地址')
    return
  }
  testingProxy.value = true
  try {
    const res = await api.post('/api/util/proxy_test', { proxy: form.proxy.trim() })
    if (res.ok) {
      toast('success', '代理连通', `延迟 ${res.latency_ms}ms`)
    } else {
      toast('error', '代理不可用', res.error || ('延迟 ' + res.latency_ms + 'ms'))
    }
  } catch (e) {
    toast('error', '测试失败', e.message)
  } finally {
    testingProxy.value = false
  }
}

async function testTelegram() {
  testingTg.value = true
  try {
    await api.post('/api/util/telegram_test')
    toast('success', '发送成功', 'Telegram 测试消息已送达')
  } catch (e) {
    toast('error', '发送失败', e.message)
  } finally {
    testingTg.value = false
  }
}

async function testTmdb() {
  testingKey.value = true
  try {
    // 先保存 Key 再测试
    await saveConfig('global_settings', { tmdb_api_key: form.tmdb_api_key })
    const res = await api.post('/api/util/tmdb_test')
    toast(res.ok ? 'success' : 'error', res.ok ? 'TMDB 连通' : 'TMDB 不可用', res.error || '配置源已生效，影视探索将加载真实数据')
  } catch (e) {
    toast('error', '测试失败', e.message)
  } finally {
    testingKey.value = false
  }
}
</script>
