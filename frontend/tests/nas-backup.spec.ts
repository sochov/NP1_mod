import {test,expect,type Page,type Route} from '@playwright/test'
const url=process.env.NCC_UI_REVIEW_URL
const target=(host:string)=>({protocol:'smb',host,port:445,share:'clips',directory:'np1',scheme:'https',hostKeySHA256:'',configured:true,usernameConfigured:true,passwordConfigured:true})
const policy=()=>({enabled:false,route:'wifi',timing:'realtime',ssid:'',targets:{wifi:target('wifi.example.invalid'),tailscale:target('nas.fixture.ts.net')}})
const initial=()=>({settings:{revision:1,policies:{video:policy(),event:policy()}},connections:{video:{},event:{}},automatic:{video:{state:'disabled',reason:'自動転送はOFFです',paused:false},event:{state:'disabled',reason:'自動転送はOFFです',paused:false}}})
const observed=(data:unknown)=>({status:'available',data})
async function open(page:Page,handler:(route:Route)=>Promise<void>){
 await page.route('**/api/v1/**',handler)
 await page.route('**/__nas-review',r=>r.fulfill({contentType:'text/html',body:'<!doctype html><html lang="ja"><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><script type="module" src="/tests/fixtures/nas-review.ts"></script></body></html>'}))
 await page.goto(url!+'/__nas-review');await expect(page.getByRole('heading',{name:'録画をNASへ保存',exact:true})).toBeVisible()
}
test.describe('NAS policy settings (isolated synthetic review)',()=>{
 test.skip(!url,'Set NCC_UI_REVIEW_URL to an isolated Vite server')
 for(const width of [320,390,1440])for(const theme of ['light','dark'])test(`${width}px ${theme} independent policies and readable inputs`,async({page},info)=>{
  const data=initial();await page.setViewportSize({width,height:900})
  await open(page,async r=>{await r.fulfill({json:observed(data)})});await page.evaluate(t=>document.documentElement.dataset.theme=t,theme)
  if(width===390||width===1440)await page.screenshot({path:info.outputPath(`nas-${width}-${theme}.png`),fullPage:true})
  for(const name of ['常時録画のバックアップ','緊急録画のバックアップ']){
   const region=page.getByRole('region',{name});await expect(region.getByLabel('自動バックアップ')).not.toBeChecked()
   await region.getByText('保存先を編集',{exact:true}).filter({visible:true}).click()
   await expect(region.getByRole('textbox',{name:'ホスト名 / IPアドレス',exact:true})).toHaveValue('wifi.example.invalid')
   await expect(region.getByLabel('パスワード').filter({visible:true})).toHaveValue('')
   expect(await region.getByRole('textbox',{name:'ホスト名 / IPアドレス',exact:true}).evaluate(el=>getComputedStyle(el).backgroundColor)).not.toBe(theme==='dark'?'rgb(255, 255, 255)':'rgb(16, 19, 24)')
  }
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 })
 test('switching routes keeps both drafts and saves only the selected recording kind',async({page})=>{
  const data=initial();let request:any
  await open(page,async route=>{
   if(route.request().method()==='PUT'){
    request=route.request().postDataJSON();data.settings.revision++
    const p=request.policies.video;data.settings.policies.video={...p,targets:Object.fromEntries(Object.entries(p.targets).map(([k,v]:any)=>[k,{...v,username:undefined,password:undefined,configured:true,usernameConfigured:true,passwordConfigured:true}]))} as any
   }
   await route.fulfill({json:observed(data)})
  })
  const video=page.getByRole('region',{name:'常時録画のバックアップ'})
  await video.getByText('保存先を編集',{exact:true}).filter({visible:true}).click()
  await video.getByRole('textbox',{name:'ホスト名 / IPアドレス',exact:true}).fill('local-new.example.invalid')
  await expect(video.getByRole('button',{name:'保存済み設定で接続確認'})).toBeDisabled()
  await video.getByRole('radio',{name:'Tailscale',exact:true}).check()
  await video.getByText('保存先を編集',{exact:true}).filter({visible:true}).click()
  await video.getByRole('textbox',{name:'ホスト名 / IPアドレス',exact:true}).fill('remote-new.fixture.ts.net')
  await video.getByRole('radio',{name:'Wi-Fi STA',exact:true}).check()
  await expect(video.getByRole('textbox',{name:'ホスト名 / IPアドレス',exact:true})).toHaveValue('local-new.example.invalid')
  await video.getByLabel('自動バックアップ').check()
  await video.getByRole('combobox',{name:'バックアップのタイミング'}).selectOption('ssid')
  await video.getByRole('textbox',{name:'接続を待つSSID'}).fill('fixture-home')
  await video.getByLabel('パスワード').filter({visible:true}).fill('fixture-only-password')
  await video.getByRole('button',{name:'設定を保存',exact:true}).click()
  await expect(page.getByText('常時録画の設定を保存しました。',{exact:true})).toBeVisible()
  expect(request.expectedRevision).toBe(1);expect(Object.keys(request.policies)).toEqual(['video'])
  expect(request.policies.video.targets.wifi.host).toBe('local-new.example.invalid');expect(request.policies.video.targets.tailscale.host).toBe('remote-new.fixture.ts.net')
  expect(request.policies.video.ssid).toBe('fixture-home');expect(request.policies.video.targets.wifi.password).toBe('fixture-only-password')
  await expect(video.getByLabel('パスワード').filter({visible:true})).toHaveValue('')
  await expect(page.getByRole('region',{name:'緊急録画のバックアップ'}).getByLabel('自動バックアップ')).not.toBeChecked()
 })
 test('cancel and resume use only the current job identity',async({page})=>{
  const data:any=initial();data.job={id:'a'.repeat(32),recordingID:'fixture-original-id',kind:'video',route:'wifi',originalName:'synthetic-recording.mp4',status:'uploading',active:true,canCancel:true,progress:{stage:'uploading',bytes:'1024',totalBytes:'4096',resumedBytes:'0'}}
  const calls:string[]=[]
  await open(page,async route=>{const method=route.request().method();calls.push(method+' '+new URL(route.request().url()).pathname)
   if(method==='DELETE'){data.job.active=false;data.job.status='cancelled';data.job.canCancel=false}
   if(method==='POST'){expect(route.request().postDataJSON()).toEqual({expectedRevision:1,recordingID:'fixture-original-id'});data.job.active=true;data.job.status='uploading';data.job.canCancel=true;data.job.progress.resumedBytes='1024'}
   await route.fulfill({json:observed(data)})
  })
  await expect(page.getByRole('progressbar')).toHaveAttribute('value','25')
  await page.getByRole('button',{name:'転送を中断',exact:true}).click()
  await page.getByRole('button',{name:'この録画から再開',exact:true}).click()
  await expect(page.getByText('1.0 KiBの検証済みデータから再開')).toBeVisible()
  expect(calls).toContain('DELETE /api/v1/nas-backup/jobs/'+'a'.repeat(32));expect(calls).toContain('POST /api/v1/nas-backup/jobs')
 })
})
