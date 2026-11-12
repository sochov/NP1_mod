<script setup lang="ts">
import {version as appVersion} from '../package.json'
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { Activity, ArrowDown, ArrowUp, ArrowUpRight, Camera, Check, ChevronDown, ChevronRight, CircleHelp, Database, Download, Folder, Gauge, Globe2, HardDrive, LayoutDashboard, LayoutGrid, LogOut, Map, MapPinned, Menu, Monitor, MoreHorizontal, Network, PanelLeft, PanelTop, Radio, RefreshCw, ScanLine, Search, Settings2, ShieldCheck, SlidersHorizontal, Sun, Usb, Users, Video, Wifi, X } from '@lucide/vue'
import ObservationCard from './components/ObservationCard.vue'
import DriveHistory from './components/DriveHistory.vue'
import PlateRecognition from './components/PlateRecognition.vue'
import DrivePlayback from './components/DrivePlayback.vue'
import CameraViewer from './components/CameraViewer.vue'
import LEDControl from './components/LEDControl.vue'
import NativeAudioSettings from './components/NativeAudioSettings.vue'
import NASBackup from './components/NASBackup.vue'
import VoicevoxSettings from './components/VoicevoxSettings.vue'
import Dashboard from './components/Dashboard.vue'
import TrafficUsage from './components/TrafficUsage.vue'
import NativeSettings from './components/NativeSettings.vue'
import OTASettings from './components/OTASettings.vue'
import CellularPage from './components/CellularPage.vue'
import WifiAPPage from './components/WifiAPPage.vue'
import TailscaleSettings from './components/TailscaleSettings.vue'
import StockServices from './components/StockServices.vue'
import {nativePages} from './services/native'
import {navLayout,dashboardMode} from './services/preferences'
import { api, bytes, date, setCSRF, type FileEntry, type Settings, type Summary } from './services/api'

const nav=[{id:'dashboard',label:'ダッシュボード',icon:LayoutDashboard,hint:'走行・通信・本体の状態'},{id:'internet',label:'4G',icon:Globe2,group:'NETWORK',hint:'回線・APN・通信量'},{id:'wifi',label:'Wi-Fi / AP',icon:Wifi,hint:'Wi-Fi接続とホットスポット'},{id:'vpn',label:'Tailscale',icon:ShieldCheck,hint:'遠隔アクセス'},{id:'camera',label:'ライブ',icon:Camera,group:'DRIVE RECORDER',hint:'カメラのライブ映像'},{id:'recordings',label:'ドラレコ再生',icon:Video,hint:'録画を日時から再生'},{id:'trips',label:'走行履歴',icon:Map,hint:'距離・経路・電波マップ'},{id:'plates',label:'ナンバー',icon:ScanLine,hint:'ナンバー認識の記録と条件'},{id:'native',label:'純正設定',icon:Settings2,group:'MANAGE',hint:'NP1本体の設定とAlexa'},{id:'system',label:'システム',icon:Monitor,hint:'本体・LED・バックアップなど'}]
const entry=(id:string)=>nav.find(n=>n.id===id)!
// The grouped forms of the menu: four groups on a phone; the menu bar folds a group only when the entries no longer fit.
const groups=[{id:'home',label:'ダッシュボード',icon:LayoutDashboard,items:[entry('dashboard')]},{id:'network',label:'ネットワーク',icon:Network,items:['internet','wifi','vpn'].map(entry)},{id:'drive',label:'ドラレコ',icon:Video,items:['camera','recordings','trips','plates'].map(entry)},{id:'manage',label:'設定',icon:Settings2,items:['native','system'].map(entry)}]
type NavEntry=(typeof nav)[number]
// Menu bar fitting. 0: everything; 1: brand and status as marks only; 2–4: fold 設定, ネットワーク, ドラレコ in turn; 5: labels without icons.
const fold=ref(0),foldAt:Record<string,number>={manage:2,network:3,drive:4}
const topItems=computed(()=>groups.flatMap(group=>{
 const start=group.id!=='home'
 if(group.items.length>1&&fold.value>=foldAt[group.id]!)return [{id:group.id,label:group.label,icon:group.icon,items:group.items as NavEntry[]|undefined,start}]
 return group.items.map((item,index)=>({id:item.id,label:item.label,icon:item.icon,items:undefined as NavEntry[]|undefined,start:start&&index===0}))
}))
const systemSection=ref('overview')
const narrowQuery=matchMedia('(max-width:860px)'),narrow=ref(narrowQuery.matches),menu=ref('')
const shell=computed(()=>narrow.value?'mobile':navLayout.value)
const sheet=computed(()=>shell.value==='mobile'?groups.find(g=>g.id===menu.value):undefined)
// The overview dashboard uses the whole window on every shell, with the map behind the floating bar.
const stage=computed(()=>page.value==='dashboard'&&dashboardMode.value==='overview')
const dashboardSwitch=computed(()=>dashboardMode.value==='overview'?'ダッシュボードを詳細表示にする':'ダッシュボードを概要表示にする')
function switchDashboard(){dashboardMode.value=dashboardMode.value==='overview'?'detail':'overview';if(page.value!=='dashboard')go('dashboard')}
const topnav=ref<HTMLElement>()
let navObserver:ResizeObserver|undefined,fitting=false,refit=false
async function fitNav(){
 if(fitting){refit=true;return}
 fitting=true
 try{do{refit=false
  for(let level=0;level<=5;level++){
   fold.value=level;await nextTick()
   const list=topnav.value?.querySelector('nav'),last=list?.lastElementChild
   if(!list||!last||last.getBoundingClientRect().right<=list.getBoundingClientRect().right+.5)break
  }
 }while(refit)}finally{fitting=false}
 if(menu.value&&shell.value==='top'&&!topItems.value.some(item=>item.id===menu.value))menu.value=''
}
const page=ref('dashboard'), advanced=ref(false), loading=ref(false), logged=ref(false), booting=ref(true), error=ref(''), notice=ref(''), connected=ref(false)
const summary=ref<Summary>({}),settings=ref<Settings>({revision:1,theme:'system',locale:'ja',favorites:['system','network','storage']}),logs=ref(''), files=ref<FileEntry[]>([]),path=ref('.'),fileSearch=ref(''), selectedVideo=ref('')
const cameraViewer=ref<InstanceType<typeof CameraViewer>>()
const dialog=ref<'logout'|'settings'|'camera'|''>(''),restoreFile=ref<Settings|null>(null),restoreDiff=ref<unknown>(),themeDraft=ref('system')
let events:EventSource|undefined,reconnect:ReturnType<typeof setTimeout>|undefined, backoff=2000

const uptime=computed(()=>{const n=summary.value.uptime?.data;if(typeof n!=='number')return '—';const h=Math.floor(n/3600),m=Math.floor(n%3600/60);return h>=24?`${Math.floor(h/24)}日 ${h%24}時間`:`${h}時間 ${m}分`})
const traffic=computed(()=>summary.value.traffic?.data as {interface:string;rxBytes:string;txBytes:string}[]|undefined)
const temp=computed(()=>{const a=summary.value.temperatures?.data as {name:string;celsius:number;source:string}[]|undefined;return a?.filter(t=>t.name.toLowerCase().includes('cpu')).slice(0,4)||a?.slice(0,4)||[]})
const fileList=computed(()=>files.value.filter(f=>f.name.toLowerCase().includes(fileSearch.value.toLowerCase())))

const systemTabs=[{id:'overview',label:'本体'},{id:'routing',label:'ルーティング'},{id:'storage',label:'SDカード'},{id:'led',label:'LED / 音声'},{id:'usb',label:'USB'},{id:'logs',label:'ログ'},{id:'nas',label:'録画バックアップ'},{id:'settings',label:'設定・バックアップ'}]
const view=computed(()=>page.value==='system'&&systemSection.value!=='overview'?systemSection.value:page.value)
function selectRoute(id:string){const [requested,tab]=id.split('?')[0].split('/');const base=requested==='clients'?'wifi':requested==='traffic'?'internet':requested==='alexa'?'native':nativePages.some(p=>p.id===requested)?'native':requested;if(systemTabs.some(t=>t.id===base)&&base!=='overview'){page.value='system';systemSection.value=base}else if(base==='system'){page.value='system';systemSection.value=systemTabs.some(t=>t.id===tab)?tab!:'overview'}else{page.value=nav.some(n=>n.id===base)?base:'dashboard';systemSection.value='overview'}}
function go(id:string){selectRoute(id);location.hash=page.value==='system'&&systemSection.value!=='overview'?'system/'+systemSection.value:page.value;menu.value=''}
function message(e:unknown){return e instanceof Error?e.message:String(e)}
async function refresh(){try{summary.value=await api<Summary>('/system/summary');connected.value=true;error.value=''}catch(e){connected.value=false;error.value=message(e);if(error.value.startsWith('401:')){logged.value=false;closeEvents();await stopCamera()}}}
async function init(){await Promise.all([refresh(),api<Settings>('/settings').then(v=>{settings.value=v;themeDraft.value=v.theme})]);openEvents();await loadPage()}
function closeEvents(){events?.close();if(reconnect)clearTimeout(reconnect)}
function openEvents(){closeEvents();if(!logged.value)return;events=new EventSource('/api/v1/events');events.addEventListener('refresh',()=>{backoff=2000;refresh()});events.onerror=()=>{connected.value=false;events?.close();reconnect=setTimeout(openEvents,backoff);backoff=Math.min(backoff*2,60000)}}
async function logout(){try{await api('/auth/logout','POST')}catch{};logged.value=false;dialog.value='';closeEvents();await stopCamera();summary.value={};logs.value='';files.value=[];selectedVideo.value=''}
async function loadPage(){if(!logged.value)return;try{if(view.value==='storage'){path.value='.';await loadFiles(path.value)}if(view.value==='logs'){const r=await fetch('/api/v1/logs');if(!r.ok)throw new Error('操作履歴を取得できません');logs.value=await r.text()}}catch(e){error.value=message(e)}}
async function loadFiles(p:string){loading.value=true;selectedVideo.value='';try{const list=await api<FileEntry[]>('/storage/files?path='+encodeURIComponent(p));files.value=list;path.value=p}catch(e){error.value=message(e)}finally{loading.value=false}}
function up(){const parts=path.value.split('/');parts.pop();loadFiles(parts.join('/')||'.')}
function fileURL(p:string){return '/api/v1/storage/file?path='+encodeURIComponent(p)}
async function saveSettings(){loading.value=true;try{const latest=await api<Settings>('/settings');settings.value=await api<Settings>('/settings','PUT',{expectedRevision:latest.revision,settings:{...latest,theme:themeDraft.value}});notice.value='設定を保存しました';dialog.value=''}catch(e){error.value=message(e)}finally{loading.value=false}}
async function exportBackup(){try{const result=await api('/backup/export','POST');download(JSON.stringify(result,null,2),'ncc-settings.json','application/json');notice.value='表示設定をエクスポートしました'}catch(e){error.value=message(e)}}
function download(data:string,name:string,type:string){const u=URL.createObjectURL(new Blob([data],{type}));const a=document.createElement('a');a.href=u;a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(u),1000)}
async function checkRestore(event:Event){try{const file=(event.target as HTMLInputElement).files?.[0];if(!file)return;if(file.size>16000)throw new Error('バックアップが大きすぎます');const value=JSON.parse(await file.text());if(value.schemaVersion!==1)throw new Error('未対応のバックアップです');restoreFile.value=value.settings;restoreDiff.value=await api('/backup/restore','POST',{schemaVersion:1,expectedRevision:settings.value.revision,dryRun:true,settings:value.settings})}catch(e){error.value=message(e)}}
async function applyRestore(){try{settings.value=await api<Settings>('/backup/restore','POST',{schemaVersion:1,expectedRevision:settings.value.revision,dryRun:false,settings:restoreFile.value});themeDraft.value=settings.value.theme;restoreFile.value=null;restoreDiff.value=undefined;notice.value='表示設定を復元しました'}catch(e){error.value=message(e)}}
async function stopCamera(){await cameraViewer.value?.stop()}
let hoverTimer:ReturnType<typeof setTimeout>|undefined
const canHover=matchMedia('(hover:hover) and (pointer:fine)')
let hoverOpened=0
function hoverMenu(id:string){if(!canHover.matches)return;clearTimeout(hoverTimer);if(id){if(menu.value!==id)hoverOpened=performance.now();menu.value=id}else hoverTimer=setTimeout(()=>menu.value='',180)}
function toggleMenu(id:string){if(menu.value===id&&performance.now()-hoverOpened<450)return;menu.value=menu.value===id?'':id}
function narrowChange(){narrow.value=narrowQuery.matches;menu.value=''}
function menuKey(event:KeyboardEvent){if(event.key==='Escape')menu.value=''}
function menuPointer(event:PointerEvent){if(menu.value&&!(event.target as Element).closest?.('.topnav-group,.tabbar,.tab-sheet'))menu.value=''}
watch(sheet,value=>document.documentElement.classList.toggle('sheet-open',!!value))
watch(topnav,element=>{navObserver?.disconnect();navObserver=undefined;if(element){navObserver=new ResizeObserver(()=>void fitNav());navObserver.observe(element)}})
watch(connected,()=>void fitNav(),{flush:'post'})
watch([systemSection,shell],()=>{if(page.value==='system'&&shell.value==='mobile')nextTick(()=>{const tabs=document.querySelector<HTMLElement>('.system-tabs'),active=tabs?.querySelector<HTMLElement>('button.active');if(tabs&&active)tabs.scrollTo({left:active.offsetLeft-(tabs.clientWidth-active.offsetWidth)/2})})})
watch([page,systemSection],async()=>{error.value='';if(page.value!=='camera')await stopCamera();await loadPage()})
watch(()=>settings.value.theme,v=>{document.documentElement.dataset.theme=v},{immediate:true})
watch(notice,v=>{if(v)setTimeout(()=>notice.value='',5000)})
onMounted(async()=>{hashChange();window.addEventListener('hashchange',hashChange);narrowQuery.addEventListener('change',narrowChange);window.addEventListener('keydown',menuKey);document.addEventListener('pointerdown',menuPointer);void document.fonts?.ready.then(fitNav);try{const s=await api<{csrf:string}>('/auth/session');setCSRF(s.csrf);logged.value=true;await init()}catch(e){logged.value=true;error.value=message(e)}finally{booting.value=false}})
function hashChange(){selectRoute(location.hash.slice(1)||'dashboard')}
onUnmounted(()=>{window.removeEventListener('hashchange',hashChange);narrowQuery.removeEventListener('change',narrowChange);window.removeEventListener('keydown',menuKey);document.removeEventListener('pointerdown',menuPointer);clearTimeout(hoverTimer);navObserver?.disconnect();document.documentElement.classList.remove('sheet-open');closeEvents();stopCamera()})
</script>

<template>
<div v-if="booting" class="boot"><span class="brand-mark">N</span><p>Control Center を開いています</p></div>
<div v-else-if="!logged" class="boot"><span class="brand-mark">N</span><p>端末への接続を確認しています</p><button class="secondary" @click="logged=true;init()">再接続</button></div>
<div v-else class="app-shell" :class="'shell-'+shell">
 <aside v-if="shell==='side'" class="sidebar"><div class="brand"><span class="brand-mark" aria-hidden="true">N</span><span class="brand-name">NP1<small>Control Center</small></span></div><nav aria-label="メインメニュー"><template v-for="n in nav" :key="n.id"><span v-if="n.group" class="nav-group">{{n.group}}</span><a :href="'#'+n.id" :class="{active:page===n.id}" :aria-current="page===n.id?'page':undefined" @click.prevent="go(n.id)"><component :is="n.icon" :size="18"/><span>{{n.label}}</span></a></template></nav><div class="sidebar-bottom"><div class="sidebar-status"><span class="connection" :class="{offline:!connected}" :title="connected?'実機データ受信中':'実機との接続を確認中'"><i></i><span>{{connected?'実機データ受信中':'実機との接続を確認中'}}</span></span><span class="connection" :class="{offline:summary.tailscale?.data?.online!==true}"><i></i><span>Tailscale {{summary.tailscale?.data?.online===true?'接続済み':summary.tailscale?.data?.backendState==='NeedsLogin'?'認証待ち':summary.tailscale?.data?.online===false?'未接続':'状態未取得'}}</span></span></div><div class="sidebar-tools"><button class="icon-button" @click="navLayout='top'" aria-label="メニューを上部に表示" title="メニューを上部に表示"><PanelTop :size="17"/></button><button class="icon-button" @click="switchDashboard" :aria-label="dashboardSwitch" :title="dashboardSwitch"><LayoutGrid v-if="dashboardMode==='overview'" :size="17"/><MapPinned v-else :size="17"/></button><button class="icon-button" @click="refresh" aria-label="状態を更新" title="状態を更新"><RefreshCw :size="17"/></button></div></div></aside>
 <div class="workspace">
 <header v-if="shell==='top'" ref="topnav" class="topnav" :class="'fold-'+fold"><div class="brand"><span class="brand-mark" aria-hidden="true">N</span><span class="brand-name">NP1<small>Control Center</small></span></div>
  <nav aria-label="メインメニュー"><template v-for="item in topItems" :key="item.id"><a v-if="!item.items" :href="'#'+item.id" :class="{active:page===item.id,'group-start':item.start}" :aria-current="page===item.id?'page':undefined" @click.prevent="go(item.id)"><component :is="item.icon" :size="17"/><span>{{item.label}}</span></a><div v-else class="topnav-group" :class="{open:menu===item.id,'group-start':item.start}" @mouseenter="hoverMenu(item.id)" @mouseleave="hoverMenu('')"><button type="button" :class="{active:item.items.some(child=>child.id===page)}" :aria-expanded="menu===item.id" aria-haspopup="true" @click="toggleMenu(item.id)"><component :is="item.icon" :size="17"/><span>{{item.label}}</span><ChevronDown class="topnav-caret" :size="14"/></button><div v-if="menu===item.id" class="topnav-menu"><div><a v-for="child in item.items" :key="child.id" :href="'#'+child.id" :class="{active:page===child.id}" :aria-current="page===child.id?'page':undefined" @click.prevent="go(child.id)"><component :is="child.icon" :size="19"/><span class="nav-text"><strong>{{child.label}}</strong><small>{{child.hint}}</small></span></a></div></div></div></template></nav>
  <div class="topnav-tools"><span class="connection" :class="{offline:!connected}" :title="connected?'実機データ受信中':'実機との接続を確認中'"><i></i><span>{{connected?'実機データ受信中':'実機との接続を確認中'}}</span></span><button class="icon-button" @click="navLayout='side'" aria-label="メニューを左側に表示" title="メニューを左側に表示"><PanelLeft :size="17"/></button><button class="icon-button" @click="switchDashboard" :aria-label="dashboardSwitch" :title="dashboardSwitch"><LayoutGrid v-if="dashboardMode==='overview'" :size="17"/><MapPinned v-else :size="17"/></button><button class="icon-button" @click="refresh" aria-label="状態を更新" title="状態を更新"><RefreshCw :size="17"/></button></div>
 </header>
 <header v-if="shell==='mobile'" class="mobile-toolbar"><span class="brand-mark" aria-hidden="true">N</span><strong class="mobile-title">{{entry(page)?.label}}</strong><span class="connection" :class="{offline:!connected}" :title="connected?'実機データ受信中':'実機との接続を確認中'"><i></i>{{connected?'受信中':'確認中'}}</span><button class="icon-button" @click="switchDashboard" :aria-label="dashboardSwitch" :title="dashboardSwitch"><LayoutGrid v-if="dashboardMode==='overview'" :size="18"/><MapPinned v-else :size="18"/></button><button class="icon-button" @click="refresh" aria-label="状態を更新"><RefreshCw :size="18"/></button></header>
 <main class="main-content" :class="{stage}"><nav v-if="page==='system'" class="system-tabs" aria-label="システム設定"><button v-for="tab in systemTabs" :key="tab.id" :class="{active:systemSection===tab.id}" :aria-current="systemSection===tab.id?'page':undefined" @click="go(tab.id==='overview'?'system':'system/'+tab.id)">{{tab.label}}</button></nav>
 <div v-if="error" class="banner error" role="alert"><CircleHelp :size="17"/><span>{{error}}</span><button class="icon-button" @click="error=''" aria-label="通知を閉じる"><X :size="15"/></button></div><div v-if="notice" class="banner success" role="status"><Check :size="17"/>{{notice}}</div>

 <template v-if="page==='dashboard'"><Dashboard :summary="summary" :phone="shell==='mobile'" @navigate="go"/></template>

 <template v-else-if="page==='internet'"><CellularPage :summary="summary" @refresh="refresh"/></template>
 <template v-else-if="page==='wifi'"><WifiAPPage :summary="summary" @refresh="refresh"/></template>
 <template v-else-if="page==='vpn'"><TailscaleSettings :observation="summary.tailscale" @refresh="refresh"/></template>
 <template v-else-if="view==='routing'"><section class="panel space-top"><div class="panel-head"><h2>ルーティング</h2><span class="tag">READ ONLY</span></div><pre>{{summary.routes?.data||'未取得'}}</pre></section></template>



 <template v-else-if="page==='native'"><NativeSettings/></template>
 <template v-else-if="page==='recordings'"><DrivePlayback/></template>
 <template v-else-if="view==='storage'"><div class="file-toolbar"><button class="secondary compact" :disabled="path==='.'||loading" @click="up"><ArrowUp :size="15"/>上へ</button><span class="mono breadcrumb-path">SD / {{path==='.'?'':path}}</span><label class="file-search"><Search :size="16"/><input v-model="fileSearch" placeholder="このフォルダーを検索" aria-label="ファイルを検索"></label></div><section class="panel"><div class="panel-head"><h2>ファイル</h2><span class="muted">最大300件 · 読み取り専用</span></div><div class="table-wrap"><table><thead><tr><th>名前</th><th>更新日時</th><th>サイズ</th><th>操作</th></tr></thead><tbody><tr v-for="f in fileList" :key="f.path"><td><button v-if="f.directory" class="file-name" @click="loadFiles(f.path)"><Folder :size="17"/>{{f.name}}<ChevronRight :size="14"/></button><span v-else class="file-name"><Video v-if="f.media==='video-candidate'" :size="17"/><HardDrive v-else :size="17"/>{{f.name}}</span></td><td class="muted nowrap">{{date(f.modified)}}</td><td class="mono nowrap">{{f.directory?'—':bytes(f.size)}}</td><td><div v-if="!f.directory" class="file-actions"><button v-if="f.media==='video-candidate'" class="text-button" @click="selectedVideo=fileURL(f.path)">再生</button><a :href="fileURL(f.path)" :download="f.name" class="icon-button" aria-label="原本をダウンロード"><Download :size="16"/></a></div></td></tr><tr v-if="!fileList.length"><td colspan="4" class="empty-line">{{loading?'読み込み中…':'ファイルがありません'}}</td></tr></tbody></table></div></section><section v-if="selectedVideo" class="panel space-top"><div class="panel-head"><h2>録画プレビュー</h2><button class="icon-button" @click="selectedVideo=''" aria-label="プレイヤーを閉じる"><X :size="17"/></button></div><video :src="selectedVideo" controls preload="metadata" class="recording-player"></video><p class="hint pad">ブラウザー非対応のコンテナ・コーデックは原本をダウンロードしてください。拡張子だけで形式を確定しません。</p></section><p class="hint">録画を時間から探す場合は「ドラレコ再生」を開いてください。ここではSDカードのファイルを確認できます。</p></template>

 <template v-else-if="page==='camera'"><CameraViewer ref="cameraViewer"/></template>

 <template v-else-if="page==='trips'"><DriveHistory/></template>
 <template v-else-if="view==='nas'"><NASBackup/></template>
 <template v-else-if="page==='plates'"><PlateRecognition/></template>

 <template v-else-if="view==='system'"><section class="overview-strip"><div><span class="eyebrow">UPTIME</span><strong>{{uptime}}</strong></div><div><span class="eyebrow">KERNEL</span><strong class="medium mono">{{summary.kernel?.data||'—'}}</strong></div><div><span class="eyebrow">LOAD AVERAGE</span><strong class="medium mono">{{String(summary.load?.data||'—').split(' ').slice(0,3).join(' / ')}}</strong></div></section><StockServices @refresh="refresh"/><section class="panel space-top"><div class="panel-head"><h2>メモリ</h2><span class="tag">/proc/meminfo</span></div><pre>{{advanced?summary.memory?.data:String(summary.memory?.data||'').split('\n').slice(0,4).join('\n')}}</pre></section></template>

 <template v-else-if="view==='led'"><div class="led-settings"><NativeAudioSettings/><LEDControl/><VoicevoxSettings/></div></template>
 <template v-else-if="view==='usb'"><ObservationCard title="USB role" :observation="summary.usb"><pre>{{summary.usb?.data}}</pre></ObservationCard><div class="banner neutral space-top"><ShieldCheck :size="18"/><span>USB host切替はADBを切断します。独立した管理経路と自動復帰を検証するまで切替は無効です。</span></div></template>

 <template v-else-if="view==='logs'"><section class="panel"><div class="panel-head"><h2>操作履歴</h2><button class="text-button" @click="loadPage"><RefreshCw :size="14"/>更新</button></div><pre class="audit-log">{{logs||'まだ操作履歴がありません'}}</pre></section></template>

 <template v-else-if="view==='settings'"><OTASettings/><div class="card-grid two"><section class="panel"><div class="panel-head"><h2>表示設定</h2><span class="muted">rev. {{settings.revision}}</span></div><div class="settings-body"><label>テーマ<select v-model="themeDraft"><option value="system">システムに合わせる</option><option value="light">ライト</option><option value="dark">ダーク</option></select></label><button class="primary" @click="dialog='settings'" :disabled="themeDraft===settings.theme">変更を確認</button><div class="layout-prefs"><label>PCのメニュー位置<select v-model="navLayout"><option value="top">上部（フローティング）</option><option value="side">左側（サイドバー）</option></select></label><label>ダッシュボードの表示<select v-model="dashboardMode"><option value="overview">概要（地図と要点）</option><option value="detail">詳細（すべての情報）</option></select></label><p>メニュー位置とダッシュボードの表示は、すぐに切り替わり、このブラウザーに保存します。スマートフォンでは下部のメニューを表示します。</p></div></div></section><section class="panel"><div class="panel-head"><h2>表示設定のバックアップ</h2><ShieldCheck :size="17"/></div><div class="settings-body"><p>表示設定をファイルに保存・復元します。</p><button class="secondary" @click="exportBackup"><Download :size="16"/>エクスポート</button><label class="upload-label">バックアップを検証<input type="file" accept="application/json,.json" @change="checkRestore"></label><template v-if="restoreDiff"><pre>{{restoreDiff}}</pre><button class="primary" @click="applyRestore">この差分で復元する</button></template></div></section></div></template>
 <template v-else><section class="panel empty-state"><Users :size="36" stroke-width="1"/><h2>実機の取得経路を調査しています</h2><p>DHCP lease・接続端末の取得方法を確認するまで、推定した端末一覧は表示しません。</p><button class="secondary" @click="go('settings')">対応状況を見る<ArrowUpRight :size="15"/></button></section></template>
 <div v-if="advanced" class="panel space-top"><div class="panel-head"><h2>取得メタデータ</h2><span class="tag">SOURCE / FRESHNESS</span></div><div class="table-wrap"><table><thead><tr><th>項目</th><th>状態</th><th>更新日時</th><th>取得元</th></tr></thead><tbody><tr v-for="(o,k) in summary" :key="k"><td>{{k}}</td><td>{{o.status}}</td><td class="nowrap">{{date(o.observedAt)}}</td><td>{{o.source}}</td></tr></tbody></table></div></div>
 <footer v-if="!stage" class="page-footer"><span>NP1 Control Center <span class="version">{{appVersion}}</span></span></footer>
 </main></div>
 <template v-if="shell==='mobile'">
  <div v-if="sheet" class="sheet-backdrop" @click="menu=''"></div>
  <div v-if="sheet" class="tab-sheet" role="dialog" :aria-label="sheet.label+'のメニュー'"><div class="tab-sheet-head"><span>{{sheet.label}}</span><button class="icon-button" @click="menu=''" aria-label="メニューを閉じる"><X :size="20"/></button></div><a v-for="item in sheet.items" :key="item.id" :href="'#'+item.id" :class="{active:page===item.id}" :aria-current="page===item.id?'page':undefined" @click.prevent="go(item.id)"><span class="tab-sheet-icon"><component :is="item.icon" :size="21"/></span><span class="nav-text"><strong>{{item.label}}</strong><small>{{item.hint}}</small></span><ChevronRight :size="18"/></a></div>
  <nav class="tabbar" aria-label="メインメニュー"><template v-for="group in groups" :key="group.id"><a v-if="group.items.length===1" :href="'#'+group.items[0]!.id" :class="{active:page===group.items[0]!.id&&!menu}" :aria-current="page===group.items[0]!.id?'page':undefined" @click.prevent="go(group.items[0]!.id)"><i><component :is="group.icon" :size="21"/></i><span>{{group.label}}</span></a><button v-else type="button" :class="{active:group.items.some(item=>item.id===page)&&!menu,open:menu===group.id}" :aria-expanded="menu===group.id" aria-haspopup="true" @click="menu=menu===group.id?'':group.id"><i><component :is="group.icon" :size="21"/></i><span>{{group.label}}</span></button></template></nav>
 </template>
 <div v-if="dialog==='settings'" class="modal-backdrop" @click.self="dialog=''"><section class="modal" role="dialog" aria-modal="true" aria-label="表示設定の変更"><div class="modal-title"><h2>表示設定を変更</h2><button class="icon-button" @click="dialog=''" aria-label="閉じる"><X :size="20"/></button></div><dl class="details"><div><dt>テーマ</dt><dd>{{settings.theme}} → {{themeDraft}}</dd></div></dl><button class="primary" @click="saveSettings" :disabled="loading">変更を適用</button></section></div>
</div>
</template>
