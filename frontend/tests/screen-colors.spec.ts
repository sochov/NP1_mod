import {test,expect} from '@playwright/test'
import {normalizeRegion,screenColors,smoothColors,type RGB} from '../src/services/screen-colors'
const settings={saturation:1,brightness:1,contrast:1,mirror:false},full={x:0,y:0,width:1,height:1}
function pixels(width:number,height:number,fill:(x:number,y:number)=>RGB){const data=new Uint8ClampedArray(width*height*4);for(let y=0;y<height;y++)for(let x=0;x<width;x++){const at=(y*width+x)*4;data.set([...fill(x,y),255],at)}return data}
test('five zones average full selected areas rather than five sampled pixels',()=>{
 const data=pixels(100,20,(x,y)=>[Math.floor(x/20)*50,y<10?0:200,20])
 expect(screenColors(data,100,20,full,settings)).toEqual([[0,100,20],[50,100,20],[100,100,20],[150,100,20],[200,100,20]])
 const crop={x:.2,y:.5,width:.6,height:.5}
 expect(screenColors(data,100,20,crop,settings)).toEqual([[50,200,20],[67,200,20],[100,200,20],[133,200,20],[150,200,20]])
})
test('fractional pixel/zone boundaries remain weighted and bounded',()=>{
 const data=pixels(2,1,x=>x===0?[255,0,0]:[0,0,255])
 expect(screenColors(data,2,1,full,settings)).toEqual([[255,0,0],[255,0,0],[128,0,128],[0,0,255],[0,0,255]])
 expect(normalizeRegion({x:-1,y:2,width:9,height:9})).toEqual({x:0,y:.99,width:1,height:1-.99})
 expect(()=>screenColors([],2,2,full,settings)).toThrow('Invalid screen frame')
})
test('color adjustments, mirror and alpha are applied to area averages',()=>{
 const data=pixels(5,1,x=>[x*50,0,0]),normal=screenColors(data,5,1,full,settings)
 expect(screenColors(data,5,1,full,{...settings,mirror:true})).toEqual([...normal].reverse())
 expect(screenColors(data,5,1,full,{...settings,brightness:0})).toEqual(Array(5).fill([0,0,0]))
 expect(screenColors(data,5,1,full,{...settings,saturation:0})[4]).toEqual([43,43,43])
 expect(screenColors(data,5,1,full,{...settings,contrast:0})[0]).toEqual([128,128,128])
 data[3]=0;expect(screenColors(data,5,1,full,settings)[0]).toEqual([0,0,0])
})
test('temporal smoothing is elapsed-time based and can be disabled',()=>{
 const from:RGB[]=[[0,0,0]],to:RGB[]=[[200,100,50]],first=smoothColors(from,to,100,200),twice=smoothColors(first,to,100,200),single=smoothColors(from,to,200,200)
 single[0]!.forEach((value,i)=>expect(twice[0]![i]).toBeCloseTo(value,9))
 expect(smoothColors(from,to,100,0)).toEqual(to);expect(smoothColors(undefined,to,0,200)).toEqual(to)
 expect(smoothColors(from,to,-10,200)).toEqual(from)
})
