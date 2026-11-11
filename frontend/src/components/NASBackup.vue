<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue'
import {Check,CloudUpload,HardDrive,ShieldCheck,Wifi} from '@lucide/vue'
import {api,bytes,type Observation} from '../services/api'

type Kind='video'|'event'
type Route='wifi'|'tailscale'
type Protocol='smb'|'sftp'|'webdav'
type Target={protocol:Protocol;host:string;port:number;share:string;directory:string;scheme?:string;hostKeySHA256?:string;configured:boolean;usernameConfigured:boolean;passwordConfigured:boolean}
type Policy={enabled:boolean;route:Route;timing:'realtime'|'ssid';ssid:string;targets:Record<Route,Target>}
type DraftTarget=Target&{username:string;password:string}
type Draft=Omit<Policy,'targets'>&{targets:Record<Route,DraftTarget>}
type Job={id:string;recordingID:string;kind:Kind;route:Route;originalName:string;status:string;active:boolean;canCancel:boolean;reason?:string;progress:{stage:string;bytes:string;totalBytes:string;resumedBytes:string};result?:{status:string;bytes:string;resumedBytes:string}}
type State={settings:{revision:number;policies:Record<Kind,Policy>};connections:Partial<Record<Kind,Partial<Record<Route,{status:string;reason?:string}>>>>;automatic:Partial<Record<Kind,{state:string;reason?:string;paused:boolean}>>;job?:Job}
const kinds:Kind[]=['video','event'],names={video:'常時録画',event:'緊急録画'},routes:Route[]=['wifi','tailscale'],routeNames={wifi:'Wi-Fi STA',tailscale:'Tailscale'}
const state=ref<State>(),drafts=ref<Partial<Record<Kind,Draft>>>({}),revision=ref(0),busy=ref(''),loading=ref(true),error=ref(''),message=ref('')
const saved=ref<Partial<Record<Kind,string>>>({})
const controller=new AbortController();let timer:ReturnType<typeof setTimeout>|undefined
let stateVersion=0
const job=computed(()=>state.value?.job),stale=computed(()=>!!state.value&&state.value.settings.revision!==revision.value)
const progress=computed(()=>job.value?Math.max(0,Math.min(100,Number(job.value.progress.bytes)/Number(job.value.progress.totalBytes)*100||0)):0)
const stages:Record<string,string>={starting:'接続を準備中',hashing:'原本を確認中','hashing-prefix':'途中データの原本を確認中','verifying-prefix':'途中データを照合中',uploading:'転送中',verifying:'保存内容を照合中','verifying-final':'保存後の照合中',finalizing:'保存を確定中',restarting:'途中データを再確認中',complete:'保存完了','already-complete':'保存済み',cancelled:'中断しました',cancelling:'中断中',interrupted:'転送が中断されました',failed:'転送できませんでした','running-elsewhere':'別の処理が転送中'}
function adopt(value:Observation<State>,reset?:Kind|'all'){
 if(value.status!=='available'||!value.data?.settings?.policies?.video||!value.data.settings.policies.event)throw new Error(value.reason||'NAS設定を取得できませんでした。')
 stateVersion++;state.value=value.data
 if(reset){for(const kind of kinds){if(reset!=='all'&&kind!==reset)continue;const p=value.data.settings.policies[kind];drafts.value[kind]={...p,targets:{wifi:{...p.targets.wifi,username:'',password:''},tailscale:{...p.targets.tailscale,username:'',password:''}}};saved.value[kind]=JSON.stringify(patch(kind))};revision.value=value.data.settings.revision}
}
async function load(reset=false){const version=stateVersion;try{const value=await api<Observation<State>>('/nas-backup','GET',undefined,true,controller.signal);if(version!==stateVersion)return;adopt(value,reset||!state.value?'all':undefined);if(reset)error.value=''}catch(e){if(!controller.signal.aborted)error.value=e instanceof Error?e.message:String(e)}finally{loading.value=false}}
function patch(kind:Kind){const d=drafts.value[kind]!;const targets=Object.fromEntries(routes.map(route=>{const t=d.targets[route];return [route,{protocol:t.protocol,host:t.host,port:t.port||0,share:t.share||'',directory:t.directory||'',scheme:t.scheme||'https',hostKeySHA256:t.hostKeySHA256||'',...(t.username?{username:t.username}:{}),...(t.password?{password:t.password}:{})}]}));return {enabled:d.enabled,route:d.route,timing:d.timing,ssid:d.ssid,targets}}
function changed(kind:Kind){return Boolean(drafts.value[kind])&&JSON.stringify(patch(kind))!==saved.value[kind]}
async function save(kind:Kind){busy.value='save-'+kind;error.value='';message.value='';try{adopt(await api<Observation<State>>('/nas-backup/policies','PUT',{expectedRevision:revision.value,policies:{[kind]:patch(kind)}},true,controller.signal),kind);message.value=names[kind]+'の設定を保存しました。'}catch(e){error.value=e instanceof Error?e.message:String(e)}finally{busy.value=''}}
async function testConnection(kind:Kind){busy.value='test-'+kind;error.value='';message.value='';try{adopt(await api<Observation<State>>('/nas-backup/test','POST',{expectedRevision:revision.value,kind,route:state.value!.settings.policies[kind].route},true,controller.signal));message.value=names[kind]+'の保存先へ接続できました。'}catch(e){error.value=e instanceof Error?e.message:String(e);await load()}finally{busy.value=''}}
async function cancel(){if(!job.value)return;busy.value='cancel';try{adopt(await api<Observation<State>>('/nas-backup/jobs/'+job.value.id,'DELETE',undefined,true,controller.signal));message.value='転送を中断しています。自動転送も一時停止します。'}catch(e){error.value=e instanceof Error?e.message:String(e)}finally{busy.value=''}}
async function resume(){if(!job.value)return;busy.value='resume';error.value='';try{adopt(await api<Observation<State>>('/nas-backup/jobs','POST',{expectedRevision:state.value!.settings.revision,recordingID:job.value.recordingID},true,controller.signal))}catch(e){error.value=e instanceof Error?e.message:String(e)}finally{busy.value=''}}
function protocolChanged(target:DraftTarget){target.port=0}
async function poll(){if(!busy.value)await load();if(!controller.signal.aborted)timer=setTimeout(poll,state.value?.job?.active?2000:8000)}
onMounted(async()=>{await load();if(!controller.signal.aborted)timer=setTimeout(poll,8000)})
onUnmounted(()=>{controller.abort();if(timer)clearTimeout(timer)})
</script>

<template>
 <div class="nas-backup">
  <section class="panel nas-intro"><div><CloudUpload :size="23"/><div><h2>録画をNASへ保存</h2><p>常時録画と緊急録画を、それぞれの条件で自動バックアップします。</p></div></div><span class="nas-original"><ShieldCheck :size="17"/>SDの原本をそのまま保存</span></section>
  <p v-if="loading" class="hint" role="status">バックアップ設定を取得しています…</p>
  <div v-if="error" class="banner error" role="alert">{{error}}</div><div v-if="message" class="banner success" role="status"><Check :size="18"/>{{message}}</div>
  <div v-if="stale" class="banner neutral">別の画面で設定が更新されています。<button class="text-button" @click="load(true)" :disabled="Boolean(busy)">設定を読み直す</button></div>
  <section v-if="job" class="panel nas-job" aria-label="録画の転送状況"><div class="panel-head"><h2><HardDrive :size="18"/>{{stages[job.status]||'転送状況'}}</h2><span class="tag">{{names[job.kind]}} · {{routeNames[job.route]}}</span></div><div class="nas-body"><p class="nas-filename">{{job.originalName}}</p><progress :value="progress" max="100" :aria-label="stages[job.status]||'転送進捗'"></progress><div class="nas-progress"><span>{{bytes(job.progress.bytes)}} / {{bytes(job.progress.totalBytes)}}</span><strong>{{progress.toFixed(0)}}%</strong></div><p v-if="Number(job.progress.resumedBytes)>0" class="hint">{{bytes(job.progress.resumedBytes)}}の検証済みデータから再開</p><p v-if="job.reason" class="hint">{{job.reason}}</p><button v-if="job.active&&job.canCancel" class="secondary" @click="cancel" :disabled="Boolean(busy)">転送を中断</button><button v-else-if="!job.active&&!['complete','already-complete'].includes(job.status)" class="primary" @click="resume" :disabled="Boolean(busy)||stale">この録画から再開</button></div></section>
  <div class="nas-policies">
   <section v-for="kind in kinds" :key="kind" class="panel nas-policy" :aria-label="names[kind]+'のバックアップ'">
    <template v-if="drafts[kind]">
     <div class="panel-head"><h2>{{names[kind]}}</h2><label class="nas-enable"><input type="checkbox" v-model="drafts[kind]!.enabled" :disabled="Boolean(busy)"><span>自動バックアップ</span><strong>{{drafts[kind]!.enabled?'ON':'OFF'}}</strong></label></div>
     <div class="nas-body">
      <fieldset :disabled="Boolean(busy)"><legend>転送する経路</legend><div class="nas-route-switch"><label v-for="route in routes" :key="route" :class="{active:drafts[kind]!.route===route}"><input type="radio" :name="'nas-route-'+kind" v-model="drafts[kind]!.route" :value="route"><Wifi v-if="route==='wifi'" :size="18"/><ShieldCheck v-else :size="18"/>{{routeNames[route]}}</label></div></fieldset>
      <label class="nas-field">バックアップのタイミング<select v-model="drafts[kind]!.timing" :disabled="Boolean(busy)"><option value="realtime">リアルタイム</option><option value="ssid">指定したWi-Fiに接続したとき</option></select></label>
      <label v-if="drafts[kind]!.timing==='ssid'" class="nas-field">接続を待つSSID<input v-model="drafts[kind]!.ssid" maxlength="32" autocomplete="off" :disabled="Boolean(busy)" placeholder="自宅などのWi-Fi名"></label>
      <p class="hint nas-timing">{{drafts[kind]!.timing==='realtime'?'録画ファイルの確定後、選んだ経路で順番に転送します。':'指定SSIDにWi-Fi STAが接続している間に転送します。'}}経路ごとの保存先は個別に保持します。</p>
      <div v-for="route in routes" :key="route" v-show="drafts[kind]!.route===route" class="nas-target">
       <h3>{{routeNames[route]}}の保存先</h3>
       <p class="nas-target-summary">{{drafts[kind]!.targets[route].host||'保存先は未設定です'}}<small v-if="drafts[kind]!.targets[route].configured">{{drafts[kind]!.targets[route].protocol.toUpperCase()}} · {{[drafts[kind]!.targets[route].share,drafts[kind]!.targets[route].directory].filter(Boolean).join('/')}}</small></p>
       <details :open="!drafts[kind]!.targets[route].configured"><summary>保存先を編集</summary><div class="nas-target-fields">
       <label class="nas-field">プロトコル<select v-model="drafts[kind]!.targets[route].protocol" :disabled="Boolean(busy)" @change="protocolChanged(drafts[kind]!.targets[route])"><option value="smb">SMB2 / SMB3</option><option value="sftp">SFTP</option><option value="webdav">WebDAV</option></select></label>
       <div class="nas-address"><label class="nas-field">ホスト名 / IPアドレス<input v-model.trim="drafts[kind]!.targets[route].host" :disabled="Boolean(busy)" autocomplete="off" placeholder="nas.example.local"></label><label class="nas-field">ポート<input type="number" min="0" max="65535" v-model.number="drafts[kind]!.targets[route].port" :disabled="Boolean(busy)" :aria-label="names[kind]+'のポート'"><small>0で標準</small></label></div>
       <label v-if="drafts[kind]!.targets[route].protocol==='smb'" class="nas-field">共有名<input v-model.trim="drafts[kind]!.targets[route].share" :disabled="Boolean(busy)" autocomplete="off" placeholder="recordings"></label>
       <label v-if="drafts[kind]!.targets[route].protocol==='webdav'" class="nas-field">接続方式<select v-model="drafts[kind]!.targets[route].scheme" :disabled="Boolean(busy)"><option value="https">HTTPS</option><option value="http">HTTP</option></select></label>
       <label class="nas-field">保存フォルダー<input v-model.trim="drafts[kind]!.targets[route].directory" :disabled="Boolean(busy)" autocomplete="off" placeholder="np1"><small>{{drafts[kind]!.targets[route].protocol==='smb'?'共有内を / で区切って指定。空欄または . で共有直下':'サーバー側の保存先パス'}}</small></label>
       <label v-if="drafts[kind]!.targets[route].protocol==='sftp'" class="nas-field">SSHホスト鍵の指紋<input v-model.trim="drafts[kind]!.targets[route].hostKeySHA256" :disabled="Boolean(busy)" placeholder="SHA256:…" autocomplete="off"></label>
       <div class="nas-credentials"><label class="nas-field">ユーザー名<input v-model="drafts[kind]!.targets[route].username" :disabled="Boolean(busy)" autocomplete="off" :placeholder="drafts[kind]!.targets[route].usernameConfigured?'保存済み・空欄で維持':'ユーザー名'"></label><label class="nas-field">パスワード<input type="password" v-model="drafts[kind]!.targets[route].password" :disabled="Boolean(busy)" autocomplete="new-password" :placeholder="drafts[kind]!.targets[route].passwordConfigured?'保存済み・空欄で維持':'パスワード'"></label></div>
       </div></details>
      </div>
      <div class="nas-actions"><button class="primary" @click="save(kind)" :disabled="Boolean(busy)||stale">{{busy==='save-'+kind?'保存中…':'設定を保存'}}</button><button class="secondary" @click="testConnection(kind)" :disabled="Boolean(busy)||stale||changed(kind)||!state?.settings.policies[kind].targets[state.settings.policies[kind].route].configured">{{busy==='test-'+kind?'接続確認中…':'保存済み設定で接続確認'}}</button></div>
      <p v-if="changed(kind)" class="hint">変更を保存すると、この設定で接続確認できます。</p>
      <p class="nas-state" role="status">{{state?.automatic[kind]?.reason||(state?.settings.policies[kind].enabled?'次の確認を待っています':'自動バックアップはOFFです')}}</p>
     </div>
    </template>
   </section>
  </div>
  <p class="hint nas-footnote">映像・音声を再圧縮せず、NASに保存した内容まで照合します。SDカードの録画は削除しません。</p>
 </div>
</template>

<style scoped>
.nas-backup{display:grid;gap:20px;min-width:0}.nas-intro{padding:22px 24px;display:flex;align-items:center;justify-content:space-between;gap:20px;flex-wrap:wrap}.nas-intro>div{display:flex;align-items:center;gap:14px}.nas-intro>div>svg{color:var(--accent);flex:none}.nas-intro h2{font-size:18px;margin:0 0 6px}.nas-intro p{font-size:14px;color:var(--muted);margin:0;line-height:1.7}.nas-original{display:flex;align-items:center;gap:7px;font-size:13px;color:var(--muted)}.nas-policies{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:20px}.nas-policy .panel-head{flex-wrap:wrap;gap:16px}.nas-policy h2{font-size:18px}.nas-enable{display:flex;align-items:center;gap:8px;min-height:44px;font-size:13px;cursor:pointer}.nas-enable input{width:18px;height:18px;accent-color:var(--accent)}.nas-enable strong{font-size:12px;background:var(--soft);padding:5px 8px;border-radius:5px}.nas-body{padding:24px;display:grid;gap:18px;min-width:0}.nas-body fieldset{border:0;margin:0;padding:0;min-width:0}.nas-body legend,.nas-field{font-size:14px;font-weight:650;line-height:1.6}.nas-body legend{margin-bottom:9px}.nas-route-switch{display:flex;gap:8px}.nas-route-switch label{display:flex;align-items:center;justify-content:center;gap:8px;border:1px solid var(--line);border-radius:6px;flex:1;min-height:46px;padding:0 8px;cursor:pointer;font-size:14px;font-weight:650;background:var(--bg)}.nas-route-switch label.active{border-color:var(--accent);background:var(--soft);color:var(--accent)}.nas-route-switch input{position:absolute;opacity:0;width:1px;height:1px}.nas-route-switch label:focus-within{outline:2px solid var(--accent);outline-offset:2px}.nas-field{display:flex;flex-direction:column;gap:7px;min-width:0}.nas-field input,.nas-field select{min-height:44px;font-size:15px;width:100%;box-sizing:border-box;min-width:0;color:var(--text);background:var(--bg);border:1px solid var(--line);border-radius:6px;padding:10px 12px;color-scheme:inherit}.nas-field small{font-size:12px;font-weight:400;color:var(--muted)}.nas-address{display:grid;grid-template-columns:minmax(0,1fr) 100px;gap:12px}.nas-credentials{display:grid;grid-template-columns:1fr 1fr;gap:12px}.nas-target{display:grid;gap:16px;border-top:1px solid var(--line);padding-top:20px;min-width:0}.nas-target h3{font-size:15px;margin:0 0 1px}.nas-timing{margin:0;line-height:1.8;font-size:13px}.nas-actions{display:flex;gap:10px;flex-wrap:wrap}.nas-actions button{min-height:44px;font-size:14px}.nas-state{margin:0;color:var(--muted);font-size:13px;line-height:1.7}.nas-job h2{display:flex;align-items:center;gap:10px;font-size:17px}.nas-job .panel-head{flex-wrap:wrap}.nas-job .nas-body{gap:12px}.nas-filename{margin:0;font-family:monospace;overflow-wrap:anywhere}.nas-job progress{width:100%;height:10px;accent-color:var(--accent)}.nas-progress{display:flex;justify-content:space-between;gap:16px;font-size:14px;font-variant-numeric:tabular-nums}.nas-job button{justify-self:start;min-height:44px}.nas-footnote{margin:0;font-size:13px;line-height:1.8}.nas-backup .banner{margin:0;flex-wrap:wrap}@media(max-width:1050px){.nas-policies{grid-template-columns:1fr}}@media(max-width:600px){.nas-intro,.nas-body{padding:18px}.nas-intro h2{font-size:17px}.nas-intro p{font-size:13px}.nas-credentials{grid-template-columns:1fr}.nas-actions{flex-direction:column}.nas-actions button{width:100%}.nas-route-switch label{font-size:13px}.nas-policy .panel-head{padding:14px 18px}.nas-job button{width:100%}}
.nas-target-summary{margin:0;font-size:15px;font-weight:650;overflow-wrap:anywhere}.nas-target-summary small{display:block;font-size:12px;color:var(--muted);font-weight:400;margin-top:6px}.nas-target details{min-width:0}.nas-target summary{cursor:pointer;min-height:44px;display:list-item;align-content:center;font-size:14px;color:var(--accent);font-weight:650}.nas-target-fields{display:grid;gap:16px;padding-top:14px;min-width:0}</style>
