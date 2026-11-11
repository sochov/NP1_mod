import {test,expect} from '@playwright/test'

test('actual Tailscale account and device name appear on dashboard and settings',async({page,request})=>{
 test.skip(process.env.NCC_TAILSCALE_HARDWARE!=='1','Set NCC_TAILSCALE_HARDWARE=1 for the attached device.')
 const response=await request.get('/api/v1/vpn/tailscale/status'),value=await response.json()
 expect(value.status).toBe('available')
 const account=value.data.accountLogin||value.data.accountName,name=value.data.deviceName
 expect(typeof account==='string'&&account.length>0).toBe(true)
 expect(typeof name==='string'&&name.length>0).toBe(true)
 await page.goto('/#dashboard')
 await expect.poll(async()=>{const text=await page.locator('main').innerText();return text.includes(account)&&text.includes(name)}).toBe(true)
 await page.goto('/#vpn')
 await expect.poll(async()=>{const text=await page.locator('main').innerText();return text.includes(account)&&text.includes(name)}).toBe(true)
 const label=value.data.hostName===name?'機器名':'ホスト名'
 await expect.poll(async()=>await page.getByLabel(label,{exact:true}).inputValue()===value.data.hostName).toBe(true)
})
