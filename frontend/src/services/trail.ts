import {api,type Observation} from './api'

import type {TrackPoint} from './playbackTrack'
type Track={points:TrackPoint[]}

const maxFixGap=5000

function validPoint(p:TrackPoint){return Number.isFinite(p.lat)&&Math.abs(p.lat)<=90&&Number.isFinite(p.lon)&&Math.abs(p.lon)<=180&&Number.isFinite(Date.parse(p.at))}
function continues(a:TrackPoint,b:TrackPoint){return a.segment===b.segment&&Date.parse(b.at)-Date.parse(a.at)<=maxFixGap}

function positionBetween(a:TrackPoint,b:TrackPoint,at:number):TrackPoint{
 const ratio=(at-Date.parse(a.at))/(Date.parse(b.at)-Date.parse(a.at))
 return {at:new Date(at).toISOString(),segment:a.segment,lat:a.lat+(b.lat-a.lat)*ratio,lon:a.lon+(b.lon-a.lon)*ratio}
}

// Clip measured segments to the selected times, interpolating endpoints only
// between consecutive fixes in the same recorded segment.
function clipTrack(points:TrackPoint[],from:number,to:number){
 const sorted=points.filter(validPoint).sort((a,b)=>Date.parse(a.at)-Date.parse(b.at))
 const segments:TrackPoint[][]=[]
 let current:TrackPoint[]=[]
 for(let i=1;i<sorted.length;i++){
  const a=sorted[i-1]!,b=sorted[i]!,start=Date.parse(a.at),end=Date.parse(b.at)
  if(!continues(a,b)){if(current.length>1)segments.push(current);current=[];continue}
  if(end<=from||start>=to||end<=start)continue
  if(!current.length)current.push(start>=from?a:positionBetween(a,b,from))
  current.push(end<=to?b:positionBetween(a,b,to))
 }
 if(current.length>1)segments.push(current)
 return segments.filter(s=>s.some(p=>p.lat!==s[0]!.lat||p.lon!==s[0]!.lon))
}

export async function recordedTrailBetween(from:number,to:number,signal:AbortSignal):Promise<TrackPoint[]>{
 if(!Number.isFinite(from)||!Number.isFinite(to)||from>=to)return []
 const segments:TrackPoint[][]=[]
 // The API accepts at most one day per query, including the bracket fixes.
 const maxSpan=24*60*60*1000-2*maxFixGap
 for(let start=from;start<to;start+=maxSpan){
  const end=Math.min(to,start+maxSpan)
  const query=new URLSearchParams({from:new Date(start-maxFixGap).toISOString(),to:new Date(end+maxFixGap).toISOString()})
  const value=await api<Observation<Track>>('/recordings/track?'+query,'GET',undefined,true,signal)
  if(!Array.isArray(value.data?.points))throw new Error('走行軌跡を取得できません')
  segments.push(...clipTrack(value.data.points,start,end))
 }
 return segments.flatMap((s,segment)=>s.map(p=>({...p,segment})))
}

// recentTrail returns the recorded driving track of the last `hours`, split into line segments
// wherever the recorder's segment changes or fixes are more than five seconds apart.
export async function recentTrail(hours=24,now=Date.now()):Promise<[number,number][][]>{
 const since=now-hours*3600000
 const query=new URLSearchParams({from:new Date(since).toISOString(),to:new Date(now).toISOString()})
 const value=await api<Observation<Track>>('/recordings/track?'+query)
 if(!Array.isArray(value.data?.points))throw new Error('走行経路を取得できません')
 const sorted=value.data.points.filter(p=>validPoint(p)&&Date.parse(p.at)>=since&&Date.parse(p.at)<=now).sort((a,b)=>Date.parse(a.at)-Date.parse(b.at))
 const segments:[number,number][][]=[]
 let last:TrackPoint|undefined
 for(const p of sorted){
  if(!last||!continues(last,p))segments.push([])
  segments[segments.length-1]!.push([p.lat,p.lon])
  last=p
 }
 return segments.filter(s=>s.length)
}
