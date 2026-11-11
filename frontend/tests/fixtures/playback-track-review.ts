import {createApp} from 'vue'
import DrivePlayback from '../../src/components/DrivePlayback.vue'
import '../../src/style.css'
import '../../src/neutral.css'
document.body.innerHTML='<div id="app" style="padding:16px;max-width:1300px;margin:auto"></div>'
createApp(DrivePlayback).mount('#app')
