import {ref,watch,type Ref} from 'vue'
// Layout choices are per browser: a phone and a PC showing the same NP1 can differ, and nothing is written to the device.
export type NavLayout='top'|'side'
export type DashboardMode='overview'|'detail'
function stored<T extends string>(key:string,allowed:readonly T[],fallback:T):Ref<T>{
 let initial=fallback
 try{const value=localStorage.getItem(key);if(allowed.includes(value as T))initial=value as T}catch{}
 const state=ref(initial) as Ref<T>
 watch(state,value=>{try{localStorage.setItem(key,value)}catch{}})
 return state
}
export const navLayout=stored<NavLayout>('ncc.nav',['top','side'],'top')
export const dashboardMode=stored<DashboardMode>('ncc.dashboard',['overview','detail'],'overview')
