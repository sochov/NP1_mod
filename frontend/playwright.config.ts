import { defineConfig } from '@playwright/test'
import { readFileSync } from 'node:fs'
// The specs written before the floating menu and the overview dashboard drive the sidebar and the detailed
// dashboard, so those two per-browser choices are preset here. shell-layout.spec.ts clears them to cover the defaults.
const targets=[process.env.NCC_BASE_URL||'http://127.0.0.1',process.env.NCC_UI_REVIEW_URL]
try{targets.push(JSON.parse(readFileSync(new URL('../tmp/device-access.json',import.meta.url),'utf8')).url)}catch{}
const origins=[...new Set(targets.filter(Boolean).map(target=>new URL(target!).origin))]
export const legacyLayout={cookies:[],origins:origins.map(origin=>({origin,localStorage:[{name:'ncc.nav',value:'side'},{name:'ncc.dashboard',value:'detail'}]}))}
export default defineConfig({testDir:'tests',outputDir:'../tmp/browser-results',timeout:60000,workers:1,use:{baseURL:process.env.NCC_BASE_URL||'http://127.0.0.1',ignoreHTTPSErrors:true,viewport:{width:1440,height:1100},trace:'off',storageState:legacyLayout},reporter:'line'})
