import {test,expect} from '@playwright/test'

const cache=(ready:number,total:number,state:string,missing:string[]=[])=>({style:1,total,ready,bytes:ready*24000,missing,state,updatedAt:'2026-11-27T01:00:00Z'})

for(const width of [1440,320])test(`voice cache shows progress and regenerates only after confirmation at ${width}px`,async({page},info)=>{
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message))
 const calls:string[]=[]
 let plate=cache(50,207,'generating',['相模','品川','あ'])
 await page.setViewportSize({width,height:900})
 await page.route('**/api/v1/**',async route=>{
  const path=new URL(route.request().url()).pathname
  if(path.endsWith('/events'))return route.fulfill({contentType:'text/event-stream',body:': fixture\n\n'})
  let data:any={}
  if(path.endsWith('/auth/session'))data={csrf:'fixture'}
  if(path.endsWith('/settings'))data={revision:1,theme:'dark',locale:'ja',favorites:[]}
  if(path.endsWith('/capabilities'))data=[]
  if(path.endsWith('/voicevox'))data={endpoint:'https://voice.example.invalid',defaultEndpoint:'https://voice.example.invalid',keyConfigured:true,apiKey:'k'}
  if(path.includes('/voicevox/cache')){
   if(route.request().method()==='POST'){calls.push(path);if(path.endsWith('/plate/regenerate'))plate=cache(0,207,'generating',['相模'])}
   data={keyConfigured:true,plate,pics:cache(45,45,'ready')}
  }
  await route.fulfill({json:data})
 })
 await page.goto('/#system/led')
 const panel=page.locator('#voice-cache')
 const plateItem=panel.getByRole('article',{name:'ナンバーの読み上げの音声キャッシュ'})
 const picsItem=panel.getByRole('article',{name:'高度化PICSの音声キャッシュ'})
 await expect(plateItem).toContainText('生成中')
 await expect(plateItem).toContainText('50 / 207')
 await expect(picsItem).toContainText('準備済み')
 await expect(picsItem).toContainText('45 / 45')
 await plateItem.getByText('未取得（157件）').click()
 await expect(plateItem).toContainText('相模、品川、あ ほか')
 await plateItem.getByRole('button',{name:'不足分を今すぐ取得'}).click()
 await expect.poll(()=>calls).toEqual(['/api/v1/voicevox/cache/plate/refresh'])
 await plateItem.getByRole('button',{name:'削除して作り直す…'}).click()
 const confirm=plateItem.getByRole('group',{name:'ナンバーの読み上げの削除の確認'})
 await expect(confirm).toContainText('207件を削除して作り直します')
 await confirm.getByRole('button',{name:'やめる'}).click()
 expect(calls).toHaveLength(1)
 await plateItem.getByRole('button',{name:'削除して作り直す…'}).click()
 await info.attach('confirm',{body:await panel.screenshot(),contentType:'image/png'})
 await confirm.getByRole('button',{name:'削除して作り直す',exact:true}).click()
 await expect.poll(()=>calls.at(-1)).toBe('/api/v1/voicevox/cache/plate/regenerate')
 await expect(plateItem).toContainText('0 / 207')
 await expect(panel.getByRole('status')).toContainText('削除しました')
 expect(calls.some(c=>c.includes('/pics/'))).toBe(false)
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true)
 expect(errors).toEqual([])
 await panel.screenshot({path:info.outputPath(`voice-cache-${width}.png`)})
})
