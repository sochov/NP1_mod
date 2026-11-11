// Cache only the public application shell. Never cache API, camera, recordings or credentials.
const CACHE='ncc-shell-v1';
self.addEventListener('install',event=>{self.skipWaiting()});
self.addEventListener('activate',event=>event.waitUntil(caches.keys().then(keys=>Promise.all(keys.filter(k=>k!==CACHE).map(k=>caches.delete(k))))));
self.addEventListener('fetch',event=>{const u=new URL(event.request.url);if(event.request.method!=='GET'||u.origin!==self.location.origin||u.pathname.startsWith('/api/'))return;event.respondWith(fetch(event.request).then(response=>{if(response.ok&&(u.pathname==='/'||u.pathname.startsWith('/assets/'))){const copy=response.clone();caches.open(CACHE).then(c=>c.put(event.request,copy))}return response}).catch(()=>caches.match(event.request)))});
