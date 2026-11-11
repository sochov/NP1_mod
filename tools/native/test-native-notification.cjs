const fs=require('node:fs');const assert=require('node:assert/strict');
(async()=>{const{instance:{exports:api}}=await WebAssembly.instantiate(fs.readFileSync(process.argv[2]),{});const memory=new Uint8Array(api.memory.buffer);const start=api.ncc_scratch_ptr();let next=start;
function string(value){const ptr=next;const bytes=Buffer.from(value+'\0');memory.set(bytes,next);next+=bytes.length;return ptr;}
const ids={'front-car-start':[79,538],'signal-change':[80,539]};
for(const[id,[index,key]]of Object.entries(ids)){
 for(let a=0;a<=2;a++)for(let b=0;b<=2;b++){next=start;assert.equal(api.ncc_validate_request(string(id),string(''+a),string(''+b)),index)}
 for(const invalid of ['', '-1','3','12','00','01','2.0','true','2;touch /tmp/x',' 1','1\n']){next=start;assert.equal(api.ncc_validate_request(string(id),string('0'),string(invalid)),0);next=start;assert.equal(api.ncc_validate_request(string(id),string(invalid),string('0')),0)}
 assert.equal(api.ncc_notification_persistent(index),key);
}
for(const id of ['79','80','privacy-mode','tailgating-detection','daily-voice','route-search','front-car-start;id','unknown']){next=start;assert.equal(api.ncc_validate_request(string(id),string('0'),string('1')),0)}
for(let value=0;value<=2;value++){next=start;assert.equal(api.ncc_notification_apply_number(string(''+value),2),value);next=start;assert.equal(api.ncc_notification_apply_number(string(''+value),1),-1)}
for(const invalid of ['3','12','2.0','-1','01','1\n']){next=start;assert.equal(api.ncc_notification_apply_number(string(invalid),2),-1)}
assert.equal(api.ncc_notification_baseline(2,0,2,0),1);assert.equal(api.ncc_notification_baseline(2,0,0,0),0);assert.equal(api.ncc_notification_baseline(2,0,0,1),1);assert.equal(api.ncc_notification_baseline(2,0,1,1),0);assert.equal(api.ncc_notification_baseline(2,0,3,1),0);
const data=Buffer.alloc(704);data.writeUInt32LE(16,4);data.writeUInt32LE(86,12);for(let i=0;i<86;i++){data.writeUInt32LE(460+i,16+8*i);data.writeInt32LE(i%3,20+8*i)}memory.set(data,start);
for(const[index,key]of Object.values(ids))assert.equal(api.ncc_notification_disk_value(start,704,index),(key-460)%3);
assert.equal(api.ncc_notification_disk_value(start,703,79),-1);assert.equal(api.ncc_notification_disk_value(start,705,79),-1);assert.equal(api.ncc_notification_disk_value(start,704,50),-1);
data.writeInt32LE(3,20+(538-460)*8);memory.set(data,start);assert.equal(api.ncc_notification_disk_value(start,704,79),-1);
data.writeInt32LE(-1,20+(539-460)*8);memory.set(data,start);assert.equal(api.ncc_notification_disk_value(start,704,80),-1);
data.writeUInt32LE(460,24);memory.set(data,start);assert.equal(api.ncc_notification_disk_value(start,704,79),-1);
console.log('PASS: compiled C two-notification 0..2 allowlist, legacy parser bridge, persistent mapping, conditional restoration, bounded backup parser');})().catch(error=>{console.error(error);process.exitCode=1});
