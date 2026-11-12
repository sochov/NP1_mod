let csrf=''
export function setCSRF(value:string){csrf=value}
export async function api<T=unknown>(path:string,method='GET',body?:unknown,retry=true,signal?:AbortSignal,keepalive=false):Promise<T>{
 const requestId=typeof crypto.randomUUID==='function'?crypto.randomUUID():Array.from(crypto.getRandomValues(new Uint8Array(16)),b=>b.toString(16).padStart(2,'0')).join('')
 const res=await fetch('/api/v1'+path,{method,credentials:'same-origin',signal,keepalive,headers:{...(body?{'Content-Type':'application/json'}:{}),...(method!=='GET'?{'X-CSRF-Token':csrf,'X-Request-ID':requestId}:{} )},body:body?JSON.stringify(body):undefined})
 if(!res.ok){let msg='リクエストに失敗しました',code='';try{const data=await res.json();msg=data.reason||msg;code=data.code||''}catch{};if(res.status===403&&code==='CSRF_REQUIRED'&&retry&&method!=='GET'){const session=await api<{csrf:string}>('/auth/session','GET',undefined,true,signal,keepalive);setCSRF(session.csrf);return api<T>(path,method,body,false,signal,keepalive)}throw new Error(`${res.status}: ${msg}`)}
 return res.json() as Promise<T>
}
// Binary POST with the same CSRF/request-id headers as api().
export async function apiUpload<T=unknown>(path:string,body:Blob,contentType:string,retry=true):Promise<T>{
 const requestId=typeof crypto.randomUUID==='function'?crypto.randomUUID():Array.from(crypto.getRandomValues(new Uint8Array(16)),b=>b.toString(16).padStart(2,'0')).join('')
 const res=await fetch('/api/v1'+path,{method:'POST',credentials:'same-origin',headers:{'Content-Type':contentType,'X-CSRF-Token':csrf,'X-Request-ID':requestId},body})
 if(!res.ok){let msg='リクエストに失敗しました',code='';try{const data=await res.json();msg=data.reason||msg;code=data.code||''}catch{};if(res.status===403&&code==='CSRF_REQUIRED'&&retry){const session=await api<{csrf:string}>('/auth/session');setCSRF(session.csrf);return apiUpload<T>(path,body,contentType,false)}throw new Error(`${res.status}: ${msg}`)}
 return res.json() as Promise<T>
}
export type Observation<T=unknown>={status:string;observedAt:string;source:string;refreshSeconds:number;verified:boolean;data:T;reason?:string}
export type Summary=Record<string,Observation<any>>
export type Capability={id:string;status:string;reason:string;writable:boolean}
export type Settings={revision:number;theme:'system'|'light'|'dark';locale:string;favorites:string[];cellularLimitBytes?:string}
export type FileEntry={name:string;path:string;size:string;modified:string;directory:boolean;media:string}
export function bytes(v:string|number){const n=Number(v);if(!Number.isFinite(n))return '—';if(n<1024)return `${n} B`;const u=['KiB','MiB','GiB','TiB'];let x=n/1024,i=0;while(x>=1024&&i<u.length-1){x/=1024;i++}return `${x.toFixed(x>=100?0:1)} ${u[i]}`}
export function date(v?:string){return v?new Date(v).toLocaleString('ja-JP',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit'}):'未取得'}
