export type MetricPoint={at:number;value:number|null}
export type MetricHistory=Record<string,MetricPoint[]>
export const resourceHistory:{value:MetricHistory}={value:{}}
export const finite=(value:unknown):value is number=>typeof value==='number'&&Number.isFinite(value)
export function remember(history:MetricHistory,key:string,at:number,value:number|null){
 if(!finite(at))return
 const points=history[key]||(history[key]=[]),last=points.at(-1)
 if(last&&at<last.at)return
 if(last?.at===at){last.value=value;return}
 points.push({at,value});while(points.length&&points[0]!.at<at-3600000)points.shift()
 if(points.length>2000)points.splice(0,points.length-2000)
}
export function chartPath(points:MetricPoint[],from:number,to:number,max:number,gap=16000){
 let drawing=false,previous=0,path=''
 for(const point of points){if(point.at<from||point.at>to)continue
  if(point.value===null||!finite(point.value)){drawing=false;continue}
  const x=(point.at-from)/(to-from)*300,y=60-Math.min(1,Math.max(0,point.value/max))*56
  path+=(drawing&&point.at-previous<=gap?'L':'M')+x.toFixed(2)+','+y.toFixed(2);drawing=true;previous=point.at
 }
 return path
}
