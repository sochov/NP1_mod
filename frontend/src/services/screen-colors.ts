export type RGB = [number, number, number]
export type ScreenRegion = {x:number;y:number;width:number;height:number}
export type ColorAdjustments = {saturation:number;brightness:number;contrast:number;mirror:boolean}
export const defaultScreenRegion:ScreenRegion={x:.1,y:.65,width:.8,height:.2}
const clamp=(value:number,min=0,max=1)=>Math.min(max,Math.max(min,Number.isFinite(value)?value:min))
export function normalizeRegion(region:ScreenRegion):ScreenRegion {
 const x=clamp(region.x,0,.99),y=clamp(region.y,0,.99)
 return {x,y,width:clamp(region.width,.01,1-x),height:clamp(region.height,.01,1-y)}
}
/** Average every pixel's intersecting area, including fractional zone boundaries. No screen data leaves this function. */
export function screenColors(data:ArrayLike<number>,width:number,height:number,region:ScreenRegion,settings:ColorAdjustments):RGB[]{
 if(!Number.isInteger(width)||!Number.isInteger(height)||width<1||height<1||data.length<width*height*4)throw new Error('Invalid screen frame')
 const r=normalizeRegion(region),top=r.y*height,bottom=(r.y+r.height)*height,result:RGB[]=[]
 for(let zone=0;zone<5;zone++){
  const left=(r.x+r.width*zone/5)*width,right=(r.x+r.width*(zone+1)/5)*width,sums=[0,0,0];let area=0
  for(let y=Math.floor(top);y<Math.ceil(bottom);y++)for(let x=Math.floor(left);x<Math.ceil(right);x++){
   const weight=(Math.min(x+1,right)-Math.max(x,left))*(Math.min(y+1,bottom)-Math.max(y,top)),index=(y*width+x)*4,alpha=clamp(Number(data[index+3])/255)
   for(let c=0;c<3;c++)sums[c]!+=Number(data[index+c])*weight*alpha
   area+=weight
  }
  const rgb=sums.map(value=>value/area),gray=.2126*rgb[0]!+.7152*rgb[1]!+.0722*rgb[2]!,saturation=clamp(settings.saturation,0,2),brightness=clamp(settings.brightness,0,2),contrast=clamp(settings.contrast,0,2)
  result.push(rgb.map(value=>Math.round(clamp(((gray+(value-gray)*saturation-127.5)*contrast+127.5)*brightness,0,255)+1e-9)) as RGB)
 }
 return settings.mirror?result.reverse():result
}
/** Exponential smoothing is based on elapsed time, independent of refresh rate. */
export function smoothColors(previous:RGB[]|undefined,next:RGB[],elapsedMs:number,smoothingMs:number):RGB[]{
 if(!previous||previous.length!==next.length||smoothingMs<=0)return next.map(rgb=>[...rgb] as RGB)
 const alpha=1-Math.exp(-Math.max(0,elapsedMs)/smoothingMs)
 return next.map((rgb,i)=>rgb.map((value,c)=>previous[i]![c]!+(value-previous[i]![c]!)*alpha) as RGB)
}
