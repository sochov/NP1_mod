import {test,expect} from '@playwright/test'
const url=process.env.NCC_UI_REVIEW_URL
const observation=(data:unknown)=>({status:'available',data,source:'isolated-navigation-fixture',observedAt:'2026-11-11T08:00:00Z',verified:false})
test('4G, Wi-Fi, Tailscale and system diagnostics stay in their own screens',async({page})=>{
 test.skip(!url,'Set NCC_UI_REVIEW_URL to the current UI')
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'})
  let value:unknown={}
  if(path.endsWith('/auth/session'))value={csrf:'fixture'}
  if(path.endsWith('/settings'))value={revision:1,theme:'light',locale:'ja',favorites:[],cellularLimitBytes:'0'}
  if(path.endsWith('/capabilities'))value=[]
  if(path.endsWith('/system/summary'))value={routes:observation('default via 192.0.2.1 dev fixture0'),wifi:observation({ssid:'Fixture Wi-Fi',wpa_state:'COMPLETED'}),tailscale:observation({online:true,backendState:'Running',ips:['100.64.0.1']})}
  if(path.endsWith('/traffic/usage'))value=observation({networks:{}})
  if(path.endsWith('/network/cellular/apn'))value={revision:0,profiles:[{id:'profile1',enabled:false,apn:'',auth:0},{id:'profile2',enabled:false,apn:'',auth:0}],applyAvailable:false,applyReason:'Synthetic apply unavailable'}
  if(path.endsWith('/network/wifi/networks'))value={status:{state:'COMPLETED',ssid:'Fixture Wi-Fi',networkID:1,ipv4:'192.0.2.2'},networks:[],saved:[],transaction:null}
  if(path.endsWith('/network/hotspot'))value=observation({settings:{revision:0,enabled:false,ssid:'Fixture AP',passphraseConfigured:false},enabled:false,dhcpReady:false,clientCount:null})
  if(path.endsWith('/device/native-settings'))value=observation({items:[{id:'tailgating-detection',label:'後続車の接近検知',group:'運転の案内',kind:'info',value:0,valueLabel:'ON',status:'available',writable:false},{id:'privacy-mode',label:'プライバシーモード',group:'プライバシー',kind:'info',value:null,status:'unknown',writable:false}]})
  await route.fulfill({contentType:'application/json',body:JSON.stringify(value)})
 })
 await page.goto(url!)
 await expect(page.getByRole('link',{name:'通信量',exact:true})).toHaveCount(0)
 await page.getByRole('link',{name:'4G',exact:true}).click()
 await expect(page.getByRole('heading',{name:'APN接続設定',exact:true})).toBeVisible()
 await expect(page.locator('.dashboard-hero,.traffic-usage')).toHaveCount(0)
 await expect(page.locator('.wifi-traffic, .main-content pre')).toHaveCount(0)
 await expect(page.locator('.traffic-speed-heading, .usage-note, .dash-section-title')).toHaveCount(0)
 await page.getByRole('link',{name:'Wi-Fi / AP',exact:true}).click()
 await expect(page.getByRole('heading',{name:'Wi-Fi接続',exact:true})).toBeVisible()
 await expect(page.getByRole('heading',{name:'ホットスポット',exact:true})).toBeVisible()
 await expect(page.getByLabel('接続先SSID',{exact:true})).toBeVisible()
 await expect(page.getByLabel('ホットスポットのSSID',{exact:true})).toBeVisible()
 await expect(page.getByRole('heading',{name:'接続端末',exact:true})).toBeVisible()
 await expect(page.getByRole('link',{name:'接続端末',exact:true})).toHaveCount(0)
 await expect(page.getByRole('heading',{name:'APN接続設定',exact:true})).toHaveCount(0)
 await page.getByRole('link',{name:'Tailscale',exact:true}).click()
 await expect(page.getByRole('heading',{name:'Tailscale / 遠隔アクセス',exact:true})).toBeVisible()
 await page.getByRole('link',{name:'システム',exact:true}).click()
 await page.getByRole('button',{name:'ルーティング',exact:true}).click()
 await expect(page.locator('.main-content pre')).toContainText('fixture0')
 await page.getByRole('link',{name:'運転サポート',exact:true}).click()
 await expect(page.locator('.native-row')).toHaveCount(1)
 await expect(page.locator('.native-row')).toContainText('後続車の接近検知')
 await page.getByRole('link',{name:'プライバシー',exact:true}).click()
 await expect(page.locator('.native-row')).toHaveCount(1)
 await expect(page.locator('.native-value')).toContainText('未取得')
})
