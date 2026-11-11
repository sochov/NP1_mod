import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
export default defineConfig({plugins:[vue()],build:{outDir:'../backend/internal/webui/dist',emptyOutDir:true},server:{proxy:{'/api':{target:'https://127.0.0.1:8443',secure:false}}}})
