<script setup lang="ts">
import { computed,onMounted,ref } from 'vue'
import { api,type Observation } from '../services/api'

type AudioData={masterVolume:number;mute:{value:number;enabled:boolean};correction:{value:number;label:string};writable:boolean;reason?:string}
type SettingID='mute'|'volume-correction'
const state=ref<Observation<AudioData>>(),loading=ref(true),busy=ref<SettingID>(),draftMute=ref(false),draftCorrection=ref(0),error=ref(''),message=ref('')
function valid(value?:Observation<AudioData>){const d=value?.data;return value?.status==='available'&&d&&Number.isInteger(d.masterVolume)&&d.masterVolume>=0&&d.masterVolume<=2147483647&&[0,1].includes(d.mute?.value)&&typeof d.mute.enabled==='boolean'&&d.mute.enabled===(d.mute.value===0)&&[0,1,2].includes(d.correction?.value)&&typeof d.writable==='boolean'}
const writable=computed(()=>valid(state.value)&&state.value?.data.writable)
const muteChanged=computed(()=>valid(state.value)&&draftMute.value!==state.value?.data.mute.enabled)
const correctionChanged=computed(()=>valid(state.value)&&draftCorrection.value!==state.value?.data.correction.value)
function receive(value:Observation<AudioData>){state.value=value;if(!valid(value))return false;draftMute.value=value.data.mute.enabled;draftCorrection.value=value.data.correction.value;return true}
async function load(){if(busy.value)return;loading.value=true;error.value='';message.value='';try{const value=await api<Observation<AudioData>>('/device/native-audio');if(!receive(value))error.value='本体の音声設定を取得できません。'}catch{state.value=undefined;error.value='本体の音声設定を取得できません。状態を更新してください。'}finally{loading.value=false}}
async function apply(id:SettingID){if(!state.value||!writable.value||loading.value||busy.value||(id==='mute'?!muteChanged.value:!correctionChanged.value))return;busy.value=id;error.value='';message.value='';const expectedValue=id==='mute'?state.value.data.mute.value:state.value.data.correction.value;const value=id==='mute'?(draftMute.value?0:1):draftCorrection.value;try{const result=await api<Observation<AudioData>>('/device/native-audio/'+id,'PUT',{expectedValue,value});if(!receive(result)||(id==='mute'?result.data.mute.value:result.data.correction.value)!==value)throw new Error();message.value=id==='mute'?'消音設定を保存しました。':'音量補正を保存しました。'}catch{state.value=undefined;error.value='変更後の設定を確認できません。状態を更新して確認してください。'}finally{busy.value=undefined}}
onMounted(load)
</script>

<template>
 <section class="panel native-audio-settings" :aria-busy="loading||Boolean(busy)">
  <div class="panel-head"><h2>本体の音声設定</h2><button class="text-button" @click="load" :disabled="loading||Boolean(busy)">状態を更新</button></div>
  <div class="settings-body">
   <p v-if="loading" class="hint" role="status">本体の設定を取得しています…</p>
   <template v-if="valid(state)">
    <div class="native-volume"><div><span class="native-audio-label">本体の音量</span><strong data-testid="native-master-volume">{{state?.data.masterVolume}}</strong></div><p class="hint">音量は本体のボタンで調整してください。</p></div>
    <p v-if="!writable" class="hint">{{state?.data.reason||'現在は設定を変更できません。'}}</p>
    <div class="native-audio-grid">
     <div class="native-audio-option">
      <h3>消音</h3>
      <fieldset :disabled="!writable||loading||Boolean(busy)"><legend class="audio-sr-only">本体の音を消す</legend><label><input type="radio" name="native-audio-mute" v-model="draftMute" :value="false">音を出す</label><label><input type="radio" name="native-audio-mute" v-model="draftMute" :value="true">消音する</label></fieldset>
      <button class="primary" @click="apply('mute')" :disabled="!writable||!muteChanged||loading||Boolean(busy)">{{busy==='mute'?'保存中…':'消音設定を保存'}}</button>
     </div>
     <div class="native-audio-option">
      <h3>音量補正</h3>
      <label class="correction-label" for="native-audio-correction">補正の強さ</label><select id="native-audio-correction" v-model.number="draftCorrection" :disabled="!writable||loading||Boolean(busy)"><option :value="0">OFF</option><option :value="1">弱</option><option :value="2">強</option></select>
      <button class="primary" @click="apply('volume-correction')" :disabled="!writable||!correctionChanged||loading||Boolean(busy)">{{busy==='volume-correction'?'保存中…':'補正設定を保存'}}</button>
     </div>
    </div>
   </template>
   <p v-else-if="!loading" class="hint">現在値は未取得です。</p>
   <p v-if="error" class="error" role="alert">{{error}}</p><p v-if="message" class="hint" role="status">{{message}}</p>
  </div>
 </section>
</template>

<style scoped>
.native-audio-settings .panel-head{gap:12px;flex-wrap:wrap}.native-audio-settings .panel-head button{min-height:44px;font-size:14px}.native-volume{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:0 0 24px;margin-bottom:24px;border-bottom:1px solid var(--line)}.native-volume>div{display:flex;align-items:baseline;gap:20px}.native-audio-label{font-size:14px;font-weight:700}.native-volume strong{font-size:30px;font-weight:750;font-variant-numeric:tabular-nums}.native-volume p{margin:0;line-height:1.8}.native-audio-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:28px}.native-audio-option{display:flex;flex-direction:column;min-width:0;align-items:flex-start}.native-audio-option h3{font-size:16px;font-weight:750;margin:0 0 16px}.native-audio-option fieldset{border:0;padding:0;margin:0 0 20px;display:flex;flex-wrap:wrap;gap:12px 24px;min-height:48px;min-width:0}.native-audio-option label{font-size:14px}.native-audio-option fieldset label{display:flex;align-items:center;gap:10px;min-height:44px;cursor:pointer}.native-audio-option input{width:18px;height:18px;accent-color:var(--accent)}.native-audio-option select{min-height:44px;width:100%;max-width:240px;font-size:15px;margin:6px 0 20px}.native-audio-option .primary{min-height:44px;margin-top:auto;font-size:14px}.native-audio-option fieldset:disabled{opacity:.6}.correction-label{color:var(--muted);line-height:1.5}.audio-sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}@media(max-width:600px){.native-volume{align-items:flex-start;flex-direction:column;gap:8px;padding-bottom:18px;margin-bottom:22px}.native-audio-grid{grid-template-columns:1fr;gap:26px}.native-audio-option+ .native-audio-option{padding-top:24px;border-top:1px solid var(--line)}.native-audio-option .primary,.native-audio-option select{width:100%;max-width:none}}
</style>
