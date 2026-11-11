const fs=require('node:fs');const assert=require('node:assert/strict');
(async()=>{const{instance:{exports:api}}=await WebAssembly.instantiate(fs.readFileSync(process.argv[2]),{});const memory=new Uint8Array(api.memory.buffer);const start=api.ncc_scratch_ptr();let next=start;
function string(value){const ptr=next;const bytes=Buffer.from(value+'\0');memory.set(bytes,next);next+=bytes.length;return ptr;}
const ids={'route-search':[68,526],'daily-voice':[77,536],'support-voice':[78,537]};
for(const[id,[index,key]]of Object.entries(ids)){
 for(let a=0;a<=5;a++)for(let b=0;b<=5;b++){next=start;assert.equal(api.ncc_validate_request(string(id),string(''+a),string(''+b)),index)}
 for(const invalid of ['', '-1','6','12','00','01','5.0','true','5;touch /tmp/x',' 1','1\n']){next=start;assert.equal(api.ncc_validate_request(string(id),string('0'),string(invalid)),0);next=start;assert.equal(api.ncc_validate_request(string(id),string(invalid),string('0')),0)}
 assert.equal(api.ncc_choice_persistent(index),key);
}
for(const id of ['68','privacy-mode','tailgating-detection','tailgating-sensitivity','front-car-start','signal-change','daily-voice;id','unknown']){next=start;assert.equal(api.ncc_validate_request(string(id),string('0'),string('1')),0)}
for(let value=0;value<=5;value++){next=start;assert.equal(api.ncc_choice_apply_number(string(''+value),2),value);next=start;assert.equal(api.ncc_choice_apply_number(string(''+value),1),-1)}
for(const invalid of ['6','12','5.0','-1','01','1\n']){next=start;assert.equal(api.ncc_choice_apply_number(string(invalid),2),-1)}
assert.equal(api.ncc_choice_baseline(5,0,5,0),1);assert.equal(api.ncc_choice_baseline(5,0,0,0),0);assert.equal(api.ncc_choice_baseline(5,0,0,1),1);assert.equal(api.ncc_choice_baseline(5,0,3,1),0);assert.equal(api.ncc_choice_baseline(5,0,6,1),0);
const data=Buffer.alloc(704);data.writeUInt32LE(16,4);data.writeUInt32LE(86,12);for(let i=0;i<86;i++){data.writeUInt32LE(460+i,16+8*i);data.writeInt32LE(i%6,20+8*i)}memory.set(data,start);
for(const[index,key]of Object.values(ids))assert.equal(api.ncc_choice_disk_value(start,704,index),(key-460)%6);
assert.equal(api.ncc_choice_disk_value(start,703,68),-1);assert.equal(api.ncc_choice_disk_value(start,705,68),-1);assert.equal(api.ncc_choice_disk_value(start,704,50),-1);
data.writeInt32LE(6,20+(536-460)*8);memory.set(data,start);assert.equal(api.ncc_choice_disk_value(start,704,77),-1);
data.writeUInt32LE(460,24);memory.set(data,start);assert.equal(api.ncc_choice_disk_value(start,704,68),-1);
console.log('PASS: compiled C three-selector 0..5 allowlist, legacy-main parser bridge, persistent mapping, conditional restoration, bounded backup parser');})().catch(error=>{console.error(error);process.exitCode=1});
