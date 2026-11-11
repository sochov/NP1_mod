<script setup lang="ts">
import { onMounted,ref } from 'vue'
import { api,type Observation } from '../services/api'
type Policy={disabled:boolean;managed:boolean;writable:boolean;token:string;scope:string}
const state=ref<Observation<Policy>>(),loading=ref(true),busy=ref(false),error=ref(''),message=ref('')
function valid(value:Observation<Policy>){return value.status==='available'&&typeof value.data?.disabled==='boolean'&&typeof value.data?.writable==='boolean'&&typeof value.data?.token==='string'&&value.data.token.length===64}
async function load(){if(busy.value)return;loading.value=true;error.value='';message.value='';try{const value=await api<Observation<Policy>>('/device/ota-policy');state.value=value;if(!valid(value))error.value=value.reason||'本体更新の設定を取得できません。'}catch{state.value=undefined;error.value='本体更新の設定を取得できません。もう一度更新してください。'}finally{loading.value=false}}
async function apply(){if(busy.value||loading.value||!state.value||!valid(state.value)||!state.value.data?.writable)return;busy.value=true;error.value='';message.value='';try{const disabled=!state.value.data.disabled;const value=await api<Observation<Policy>>('/device/ota-policy','PUT',{expectedToken:state.value.data.token,disabled});state.value=value;if(!valid(value)||value.data?.disabled!==disabled)throw new Error();message.value=disabled?'本体更新の新しいデータ取得を停止しました。':'本体更新のデータ取得を有効にしました。'}catch{state.value=undefined;error.value='変更後の設定を確認できません。状態を更新してください。'}finally{busy.value=false}}
onMounted(load)
</script>
<template>
 <section class="panel space-top ota-settings" :aria-busy="loading||busy">
  <div class="panel-head"><h2>本体の自動更新</h2><span v-if="state&&valid(state)" class="tag">{{state.data?.disabled?'新規取得を停止中':'取得を許可'}}</span></div>
  <div class="settings-body">
   <p>NP1の更新データを新しく取得するかを設定します。導入時は停止しています。</p>
   <p class="hint">開始済みのダウンロードや更新処理は中断しません。音声・ナビなど、通常のサービス通信は対象外です。</p>
   <p v-if="loading" class="hint" role="status">設定を読み込んでいます…</p>
   <template v-if="state&&valid(state)">
    <p v-if="!state.data?.writable" class="hint">管理記録を確認できないため、ここからは変更できません。</p>
    <p v-else-if="state.data.disabled" class="hint">更新でNP1modの動作が変わる可能性があるため、有効化は推奨していません。</p>
    <div class="ota-actions"><button :class="state.data?.disabled?'secondary':'primary'" @click="apply" :disabled="busy||loading||!state.data?.writable">{{busy?'変更中…':state.data?.disabled?'取得を有効にする（非推奨）':'新しい更新データの取得を停止'}}</button><button class="text-button" @click="load" :disabled="busy||loading">状態を更新</button></div>
   </template>
   <button v-else-if="!loading" class="secondary" @click="load">状態を更新</button>
   <p v-if="error" class="error" role="alert">{{error}}</p><p v-if="message" class="hint" role="status">{{message}}</p>
  </div>
 </section>
</template>
<style scoped>
.ota-settings{margin-bottom:24px}.ota-actions{display:flex;align-items:center;flex-wrap:wrap;gap:14px;margin-top:22px}.ota-actions button{min-height:44px;font-size:13px}.ota-settings p{line-height:1.8;max-width:720px}@media(max-width:520px){.ota-actions{align-items:stretch;flex-direction:column;gap:10px}.ota-actions button{width:100%;justify-content:center}}
</style>
