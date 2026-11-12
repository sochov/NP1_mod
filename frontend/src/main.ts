import { createApp } from 'vue'
import App from './App.vue'
import './style.css'
import './shell.css'
import './dashboard.css'
createApp(App).mount('#app')
if ('serviceWorker' in navigator && location.protocol === 'https:') navigator.serviceWorker.register('/sw.js').catch(()=>{})
