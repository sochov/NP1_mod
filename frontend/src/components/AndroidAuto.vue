<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue'
import {Bluetooth,MonitorSmartphone,Wifi} from '@lucide/vue'
import {api} from '../services/api'

// Android Auto projection. The projection runs as its own service
// (np1-control-android-auto); this page only saves its settings and shows the
// status it publishes.
type Settings={enabled:boolean;maxResolution:string;fps:number;bitrateKbps:number;theme:string;mapZoom:number;bluetooth:boolean;revision:number}
type BTHeadUnit={address:string;pairedAt:string;lastSeen?:string}
type BTStatus={state:string;reason?:string;pairingUntil?:string;headUnits:BTHeadUnit[];peer?:string;network?:string;lastError?:string;lastErrorAt?:string}
type Video={width:number;height:number;fps:number;marginWidth:number;marginHeight:number}
type Stream={fps:number;kbps:number;frames:number;dropped:number;renderMs:number;encodeMs:number;width:number;height:number;stalls:number;touches:number;maxUnacked:number}
type Status={state:string;since:string;listen?:string;reason?:string;lastError?:string;lastErrorAt?:string;certificate:string;headUnit?:{make:string;model:string;headUnitMake:string;headUnitModel:string;displayName:string};video?:Video;stream?:Stream;sessions:number;peer?:string;bluetooth?:BTStatus}
type Overview={settings:Settings;service:'running'|'unavailable'|'not-installed';status?:Status}

const overview=ref<Overview>(),draft=ref<Settings>(),busy=ref(false),error=ref(''),message=ref('')
let timer:ReturnType<typeof setInterval>|undefined
const errorText=(e:unknown)=>e instanceof Error?e.message.replace(/^\d+: /,''):String(e)
const time=(v?:string)=>v?new Date(v).toLocaleString('ja-JP',{timeZone:'Asia/Tokyo',month:'numeric',day:'numeric',hour:'2-digit',minute:'2-digit',second:'2-digit'}):'—'

const stateLabel:Record<string,string>={disabled:'停止中',waiting:'接続待ち',connecting:'接続中',projecting:'投影中',background:'ナビ側の画面を表示中',error:'エラー'}
const status=computed(()=>overview.value?.status)
const badge=computed(()=>{
 const o=overview.value
 if(!o)return {text:'読み込み中',tone:''}
 if(o.service==='not-installed')return {text:'サービス未導入',tone:'warn'}
 if(o.service!=='running')return {text:'サービス停止中',tone:'warn'}
 const s=o.status?.state||'disabled'
 return {text:stateLabel[s]||s,tone:s==='projecting'?'ok':s==='error'?'warn':''}
})
const headUnit=computed(()=>{const h=status.value?.headUnit;if(!h)return '';const car=[h.make,h.model].filter(Boolean).join(' ');const unit=[h.headUnitMake,h.headUnitModel].filter(Boolean).join(' ');return car&&unit&&car!==unit?`${car}（${unit}）`:car||unit||h.displayName||'不明'})
const btLabel:Record<string,string>={off:'無効',unavailable:'純正Bluetoothを利用できません',idle:'待機中',pairing:'ペアリング受付中',connecting:'ナビを呼び出し中',handshake:'Wi-Fi情報を受信中',joining:'ナビのWi-Fiに接続中',projecting:'投影中'}
const bt=computed(()=>status.value?.bluetooth)
const btBusy=ref(false)
const pairingLeft=computed(()=>{const u=bt.value?.pairingUntil;return u?Math.max(0,Math.round((new Date(u).getTime()-now.value)/1000)):0})
const now=ref(Date.now())
async function btCall(path:string,method:'POST'|'DELETE',done:string){
 btBusy.value=true;error.value='';message.value=''
 try{await api(path,method);message.value=done;await load()}catch(e){error.value=errorText(e)}finally{btBusy.value=false}
}
const startPairing=()=>btCall('/android-auto/bluetooth/pairing','POST','ペアリングを受け付けています。ナビのBluetooth設定で本体を選んでください。')
const stopPairing=()=>btCall('/android-auto/bluetooth/pairing','DELETE','ペアリングの受付を終了しました。')
function removeHeadUnit(a:string){if(confirm(`${a} の登録を削除しますか？`))btCall('/android-auto/bluetooth/head-units/'+encodeURIComponent(a),'DELETE','登録を削除しました。')}
const dirty=computed(()=>!!draft.value&&!!overview.value&&JSON.stringify(draft.value)!==JSON.stringify(overview.value.settings))

async function load(keepDraft=true){
 try{
  const o=await api<Overview>('/android-auto')
  if(!o||typeof o.settings?.revision!=='number')throw new Error('Android Autoの設定を取得できませんでした')
  const wasDirty=dirty.value
  overview.value=o
  if(!keepDraft||!draft.value||!wasDirty)draft.value={...o.settings}
 }catch(e){error.value=errorText(e)}
}
async function save(){
 if(!draft.value||!overview.value)return
 busy.value=true;error.value='';message.value=''
 try{
  const saved=await api<Settings>('/android-auto/settings','PUT',{expectedRevision:overview.value.settings.revision,settings:{...draft.value,revision:overview.value.settings.revision}})
  overview.value={...overview.value,settings:saved};draft.value={...saved}
  message.value=saved.enabled||saved.bluetooth?'保存しました。':'保存しました。Android Autoの投影を停止します。'
  await load(false)
 }catch(e){error.value=errorText(e)}finally{busy.value=false}
}
onMounted(()=>{load(false);timer=setInterval(()=>{now.value=Date.now();if(!document.hidden&&!busy.value)load()},3000)})
onUnmounted(()=>{if(timer)clearInterval(timer)})
</script>

<template>
 <div class="android-auto">
  <section class="panel" :aria-busy="busy">
   <div class="panel-head"><h2><MonitorSmartphone :size="18"/>Android Auto</h2><span class="tag" :class="badge.tone">{{badge.text}}</span></div>
   <div class="settings-body">
    <p class="hint">NP1をスマートフォンの代わりにして、ヘッドユニット（ナビ）の画面にAndroidアプリの横画面ダッシュボード（地図・前方カメラ・ナンバー一覧）を表示します。描画と送信は専用サービスで行い、映像は本体のハードウェアエンコーダーでH.264に変換します。</p>
    <dl v-if="status" class="aa-facts">
     <dt>状態</dt><dd>{{stateLabel[status.state]||status.state}}<template v-if="status.reason"> · {{status.reason}}</template><small> （{{time(status.since)}}から）</small></dd>
     <dt>待ち受け</dt><dd>{{status.listen||'—'}}</dd>
     <template v-if="status.headUnit"><dt>ヘッドユニット</dt><dd>{{headUnit}}<small v-if="status.peer"> · {{status.peer}}</small></dd></template>
     <template v-if="status.video"><dt>映像</dt><dd>{{status.video.width}}×{{status.video.height}}（ナビ側 {{status.video.fps}}fps）<template v-if="status.video.marginWidth||status.video.marginHeight"> · 余白 {{status.video.marginWidth}}×{{status.video.marginHeight}}</template></dd></template>
     <template v-if="status.stream"><dt>送信</dt><dd>{{status.stream.fps.toFixed(1)}} fps · {{Math.round(status.stream.kbps).toLocaleString()}} kbps · 描画 {{status.stream.renderMs.toFixed(1)}} ms · 符号化 {{Math.round(status.stream.encodeMs)}} ms<small> · 送出 {{status.stream.frames.toLocaleString()}} / 間引き {{status.stream.dropped.toLocaleString()}}</small></dd></template>
     <dt>証明書</dt><dd>{{status.certificate==='owner'?'所有者が配置した証明書':'自己署名（本体で生成）'}}</dd>
     <template v-if="status.lastError"><dt>直近のエラー</dt><dd class="error-text">{{status.lastError}}<small> （{{time(status.lastErrorAt)}}）</small></dd></template>
    </dl>
    <p v-else-if="overview?.service==='not-installed'" class="hint">np1-control-android-auto.service が導入されていません。このバージョンのNP1CCを導入し直してください。</p>
    <p v-else-if="overview" class="hint">投影サービスの状態を取得できません。サービスが起動していない可能性があります。</p>

    <form v-if="draft" class="aa-form" @submit.prevent="save">
     <label class="aa-switch"><input v-model="draft.enabled" type="checkbox">ヘッドユニットアプリからの接続を受け付ける</label>
     <label class="aa-switch"><input v-model="draft.bluetooth" type="checkbox">登録済みのナビへBluetoothで接続する（無線Android Auto）</label>
     <div class="aa-grid">
      <label>最大解像度<select v-model="draft.maxResolution"><option value="800x480">800×480（軽量）</option><option value="1280x720">1280×720</option><option value="1920x1080">1920×1080</option></select></label>
      <label>フレームレート上限<select v-model.number="draft.fps"><option :value="10">10 fps</option><option :value="15">15 fps</option><option :value="20">20 fps</option><option :value="30">30 fps</option></select></label>
      <label>ビットレート（800×480相当）<select v-model.number="draft.bitrateKbps"><option :value="1500">1.5 Mbps</option><option :value="2500">2.5 Mbps</option><option :value="4000">4 Mbps</option><option :value="6000">6 Mbps</option></select></label>
      <label>画面の色<select v-model="draft.theme"><option value="auto">自動（日の出・日の入り）</option><option value="light">ライト</option><option value="dark">ダーク</option></select></label>
      <label>地図の縮尺<select v-model.number="draft.mapZoom"><option :value="14">広域</option><option :value="15">やや広域</option><option :value="16">標準</option><option :value="17">詳細</option></select></label>
     </div>
     <p class="hint">ナビが提示する解像度のうち、上限以下で最も大きいものを使います。前方カメラは純正の出力（約10fps）を使うため、変化のない間は送信を間引きます。大きい解像度ほどビットレートを比例して上げます。</p>
     <div class="aa-actions"><span class="muted">rev. {{overview?.settings.revision}}</span><button type="button" class="secondary" :disabled="busy||!dirty" @click="draft={...overview!.settings}">元に戻す</button><button type="submit" class="primary" :disabled="busy||!dirty">{{busy?'保存中…':'保存'}}</button></div>
    </form>
    <p v-if="error" class="error" role="alert">{{error}}</p><p v-if="message" class="hint" role="status">{{message}}</p>
   </div>
  </section>

  <div class="card-grid two">
   <section class="panel">
    <div class="panel-head"><h2><Wifi :size="18"/>ヘッドユニットアプリでの接続</h2><span class="tag ok">利用可能</span></div>
    <div class="settings-body">
     <ol class="aa-steps">
      <li>ヘッドユニット（Androidナビ）をNP1のホットスポットに接続します。SSIDとパスワードは「Wi-Fi / AP」画面で確認できます。一度接続すれば以後は自動で接続されます。</li>
      <li>ヘッドユニットアプリ（Open Headunit / Headunit Revived など）の接続方式を「Headunit Server」（ネットワーク上のスマートフォンを自動検索）にします。</li>
      <li>アプリがNP1（{{status?.listen||'192.168.226.1:5277'}}）を見つけて接続すると、ここの状態が「投影中」になります。</li>
     </ol>
     <p class="hint">同時に接続できるヘッドユニットは1台です。ホットスポットとこの本体（adb転送）以外からの接続は受け付けません。</p>
    </div>
   </section>
   <section class="panel">
    <div class="panel-head"><h2><Bluetooth :size="18"/>市販ナビの無線Android Auto</h2><span class="tag" :class="bt?.state==='projecting'?'ok':bt?.state==='unavailable'?'warn':''">{{bt?btLabel[bt.state]||bt.state:'—'}}</span></div>
    <div class="settings-body">
     <p>ナビとBluetoothでペアリングし、ナビから受け取ったWi-Fiへ接続して投影します。Bluetoothは純正サービスを介して操作し、純正のスマートフォン連携やPICSの受信はそのまま動きます。</p>
     <dl v-if="bt" class="aa-facts">
      <dt>状態</dt><dd>{{btLabel[bt.state]||bt.state}}<template v-if="bt.reason"> · {{bt.reason}}</template><template v-if="bt.peer"> · {{bt.peer}}</template></dd>
      <template v-if="bt.network"><dt>ナビのWi-Fi</dt><dd>{{bt.network}}</dd></template>
      <template v-if="bt.lastError"><dt>直近のエラー</dt><dd class="error-text">{{bt.lastError}}<small> （{{time(bt.lastErrorAt)}}）</small></dd></template>
     </dl>
     <div class="aa-actions">
      <span v-if="bt?.pairingUntil" class="muted">受付中 残り{{pairingLeft}}秒</span>
      <button v-if="bt?.pairingUntil" type="button" class="secondary" :disabled="btBusy" @click="stopPairing">受付を終了</button>
      <button v-else type="button" class="primary" :disabled="btBusy||!overview?.settings.bluetooth||!bt||bt.state==='unavailable'||bt.state==='off'" @click="startPairing">ナビを登録</button>
     </div>
     <p class="hint" v-if="!overview?.settings.bluetooth">登録と接続には、上の「登録済みのナビへBluetoothで接続する」を有効にして保存してください。</p>
     <p class="hint" v-else>「ナビを登録」を押してから2分以内に、ナビのBluetooth設定（スマートフォンの追加）で本体（NP1-xxxx）を選んでください。確認コードは本体側で自動的に承認します。</p>
     <table v-if="bt?.headUnits.length" class="aa-units"><thead><tr><th>登録済みのナビ</th><th>登録</th><th>最終接続</th><th></th></tr></thead>
      <tbody><tr v-for="h in bt.headUnits" :key="h.address"><td>{{h.address}}</td><td>{{time(h.pairedAt)}}</td><td>{{time(h.lastSeen)}}</td><td><button type="button" class="secondary" :disabled="btBusy" @click="removeHeadUnit(h.address)">削除</button></td></tr></tbody>
     </table>
     <p class="hint">現在はナビからWi-Fiの情報を受け取るところまで対応しています。ナビのWi-Fiへの接続は今後対応します。また市販ナビの多くはスマートフォン側の証明書を確認するため、本体で生成した証明書では投影を拒否される可能性があります。</p>
    </div>
   </section>
  </div>
 </div>
</template>

<style scoped>
.android-auto{display:grid;gap:16px}
.tag.ok{color:var(--ok);border-color:currentColor}
.tag.warn{color:var(--warn);border-color:currentColor}
.aa-facts{display:grid;grid-template-columns:max-content 1fr;gap:6px 16px;margin:0}
.aa-facts dt{color:var(--muted)}
.aa-facts dd{margin:0;overflow-wrap:anywhere}
.aa-facts small{color:var(--muted)}
.error-text{color:var(--danger)}
.aa-form{display:grid;gap:12px}
.aa-switch{display:flex;align-items:center;gap:8px;font-weight:600}
.aa-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(200px,1fr));gap:12px}
.aa-grid label{display:grid;gap:4px}
.aa-actions{display:flex;align-items:center;justify-content:flex-end;gap:8px;flex-wrap:wrap}
.aa-actions .muted{margin-right:auto}
.aa-units{width:100%;border-collapse:collapse}
.aa-units th,.aa-units td{text-align:left;padding:6px 8px;border-bottom:1px solid var(--line);overflow-wrap:anywhere}
.aa-units th{color:var(--muted);font-weight:500}
.aa-steps{margin:0;padding-left:20px;display:grid;gap:8px}
h2 svg{vertical-align:-3px;margin-right:6px}
@media (max-width:560px){.aa-facts{grid-template-columns:1fr}.aa-facts dt{margin-top:6px}}
</style>
