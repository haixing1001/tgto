<template>
  <div class="h-full flex items-center justify-center bg-gradient-to-br from-slate-100 to-slate-200 dark:from-[#0b0d12] dark:to-[#151a23]">
    <div class="w-full max-w-sm px-4">
      <div class="card p-8 shadow-xl">
        <div class="flex flex-col items-center mb-6">
          <div class="w-14 h-14 rounded-2xl bg-brand-500 flex items-center justify-center text-white text-2xl font-bold shadow-lg shadow-brand-500/30">T</div>
          <h1 class="mt-4 text-xl font-bold text-slate-800 dark:text-slate-100">Tgto</h1>
          <p class="text-xs text-slate-400 mt-1">网盘媒体自动化管理平台</p>
          <span class="badge mt-2 bg-brand-500/10 text-brand-600 dark:text-brand-500">v{{ state.version }} · 轻量 NAS 单二进制版</span>
        </div>

        <form @submit.prevent="onSubmit" class="space-y-4">
          <div>
            <label class="label">管理员账号</label>
            <input v-model="form.username" class="input" autocomplete="username" />
          </div>
          <div>
            <label class="label">密码</label>
            <input v-model="form.password" type="password" class="input" autocomplete="current-password" />
          </div>
          <div v-if="error" class="text-xs text-rose-500">{{ error }}</div>
          <button type="submit" class="btn-primary w-full py-2.5" :disabled="loading">
            {{ loading ? '登录中...' : '登 录' }}
          </button>
        </form>

        <div class="mt-5 text-center text-[10px] text-slate-400 leading-relaxed">
          115 / 123云盘 / 光鸭云盘 / 天翼云盘 · TG 频道监控转存<br />
          TMDB / MusicBrainz 刮削整理 · 全量与增量 STRM
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { state, login, navigate, toast } from '../store.js'

const form = reactive({ username: 'admin', password: '123' })
const loading = ref(false)
const error = ref('')

async function onSubmit() {
  if (!form.username.trim() || !form.password) {
    error.value = '请输入账号和密码'
    return
  }
  loading.value = true
  error.value = ''
  try {
    await login(form.username.trim(), form.password)
    toast('success', '欢迎回来', `已登录：${form.username}`)
    navigate('global-settings')
  } catch (e) {
    error.value = e.message || '登录失败'
  } finally {
    loading.value = false
  }
}
</script>
