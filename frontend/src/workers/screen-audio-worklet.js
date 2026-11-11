// Same-origin AudioWorklet. Downmix to mono and area-resample to 24 kHz PCM16LE.
// This processor outputs silence locally; only bounded PCM chunks leave its port.
class ScreenAudioProcessor extends AudioWorkletProcessor {
 constructor(){super();this.ratio=sampleRate/24000;this.sum=0;this.weight=0;this.index=0;this.buffer=new ArrayBuffer(9600);this.view=new DataView(this.buffer)}
 append(value){const clipped=Number.isFinite(value)?Math.min(1,Math.max(-1,value)):0;this.view.setInt16(this.index*2,Math.round(clipped*(clipped<0?32768:32767)),true);this.index++;if(this.index===4800){this.port.postMessage(this.buffer,[this.buffer]);this.buffer=new ArrayBuffer(9600);this.view=new DataView(this.buffer);this.index=0}}
 process(inputs,outputs){
  for(const output of outputs)for(const channel of output)channel.fill(0)
  const channels=inputs[0];if(!channels?.length)return true
  for(let i=0;i<channels[0].length;i++){
   let value=0;for(const channel of channels)value+=channel[i]||0;value/=channels.length
   let remaining=1;while(remaining>1e-10){const take=Math.min(remaining,this.ratio-this.weight);this.sum+=value*take;this.weight+=take;remaining-=take;if(this.weight>=this.ratio-1e-10){this.append(this.sum/this.ratio);this.sum=0;this.weight=0}}
  }
  return true
 }
}
registerProcessor('screen-audio-pcm',ScreenAudioProcessor)
