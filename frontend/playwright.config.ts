import { defineConfig } from '@playwright/test'
export default defineConfig({testDir:'tests',outputDir:'../tmp/browser-results',timeout:60000,workers:1,use:{baseURL:process.env.NCC_BASE_URL||'http://127.0.0.1',ignoreHTTPSErrors:true,viewport:{width:1440,height:1100},trace:'off'},reporter:'line'})
