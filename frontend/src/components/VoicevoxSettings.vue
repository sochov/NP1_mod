<script setup lang="ts">
import {onMounted,ref} from 'vue'
import {api} from '../services/api'
import type {VoicevoxConfig} from '../services/plate'

// Owner settings for the WEB版VOICEVOX API used by plate announcements.
// The key is write-only: the server only reports whether one is stored.
const config=ref<VoicevoxConfig>(),endpoint=ref(''),key=ref(''),busy=ref(false),error=ref(''),message=ref(''),points=ref('')
const errorText=(e:unknown)=>e instanceof Error?e.message:String(e)
function receive(c:VoicevoxConfig){if(typeof c?.endpoint!=='string'||typeof c.defaultEndpoint!=='string')throw new Error('API設定を取得できませんでした。画面を更新してください。');config.value=c;endpoint.value=c.endpoint;key.value=''}
async function load(){error.value='';try{receive(await api<VoicevoxConfig>('/voicevox'))}catch(e){error.value=errorText(e)}}
async function save(clearKey=false){busy.value=true;error.value='';message.value='';points.value='';try{const body:Record<string,string>={endpoint:endpoint.value.trim()};if(clearKey)body.key='';else if(key.value.trim())body.key=key.value.trim();receive(await api<VoicevoxConfig>('/voicevox','PUT',body));message.value=clearKey?'APIキーを削除しました':'保存しました。ナンバーの発話音声を生成します'}catch(e){error.value=errorText(e)}finally{busy.value=false}}
async function check(){busy.value=true;error.value='';message.value='';try{const p=await api<{points:number;resetInHours:number}>('/voicevox/check','POST');points.value=`残り ${p.points.toLocaleString()} ポイント（約${Math.round(p.resetInHours)}時間後に回復）`}catch(e){error.value=errorText(e)}finally{busy.value=false}}
onMounted(load)
</script>

<template>
 <section id="voicevox" class="panel voicevox-settings" :aria-busy="busy">
  <div class="panel-head"><h2>音声合成（VOICEVOX API）</h2></div>
  <div class="settings-body">
   <p class="hint">ナンバー画面の発話で使う音声は、条件を保存したときにこのAPIで生成して本体に保存します。走行中に検知した他の車のナンバーは送信しません。</p>
   <form class="voicevox-form" @submit.prevent="save()">
    <label>エンドポイント<input v-model="endpoint" type="url" required autocomplete="off" spellcheck="false" :placeholder="config?.defaultEndpoint"></label>
    <label>APIキー<input v-model="key" type="password" autocomplete="off" spellcheck="false" :placeholder="config?.keyConfigured?'設定済み（変更する場合のみ入力）':'未設定'"></label>
    <p class="hint">キーは <a href="https://su-shiki.com/api/" target="_blank" rel="noopener">su-shiki.com/api</a> で発行できます。保存したキーは画面に表示しません。<template v-if="config&&endpoint!==config.defaultEndpoint"> 既定のエンドポイントは <code>{{config.defaultEndpoint}}</code> です。</template></p>
    <div class="voicevox-actions">
     <span class="voicevox-state" :class="config?.keyConfigured?'on':'warn'">{{config?.keyConfigured?'APIキー設定済み':'APIキー未設定'}}</span>
     <button type="button" class="secondary" :disabled="busy||!config?.keyConfigured" @click="check">接続を確認</button>
     <button v-if="config?.keyConfigured" type="button" class="secondary" :disabled="busy" @click="save(true)">キーを削除</button>
     <button type="submit" class="primary" :disabled="busy||!endpoint.trim()">{{busy?'保存中…':'保存'}}</button>
    </div>
   </form>
   <p v-if="points" class="hint" role="status">{{points}}</p>
   <p v-if="error" class="error" role="alert">{{error}}</p><p v-if="message" class="hint" role="status">{{message}}</p>
  </div>
 </section>
</template>

<style scoped>
.voicevox-form{display:grid;gap:12px;max-width:640px}
.voicevox-form label{display:grid;gap:4px;font-size:13px}
.voicevox-form input{min-width:0}
.voicevox-actions{display:flex;flex-wrap:wrap;align-items:center;gap:8px}
.voicevox-state{font-size:13px;font-weight:600;margin-right:auto}
.voicevox-state.on{color:var(--accent)}
.voicevox-state.warn{color:#9a6a1f}
:global([data-theme=dark]) .voicevox-state.warn{color:#e2b56c}
@media (prefers-color-scheme:dark){:global(:root:not([data-theme=light]):not([data-theme=dark])) .voicevox-state.warn{color:#e2b56c}}
</style>
