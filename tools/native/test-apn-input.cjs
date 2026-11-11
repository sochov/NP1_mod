// Pure host test. Fixture credentials are synthetic and never sent to hardware.
const fs = require('node:fs');
const assert = require('node:assert/strict');
(async () => {
 const { instance } = await WebAssembly.instantiate(fs.readFileSync(process.argv[2]), {});
 const api = instance.exports;
 const memory = new Uint8Array(api.memory.buffer);
 const start = api.ncc_apn_buffer();
 function fixture() {
  const value = Buffer.alloc(317);
  value.write('NCCAPN1\n');
  for (let i=0;i<2;i++) {
   const p=8+154*i; value[p]=i+1; value[p+1]=i;
   value.write(`fixture-${i}.invalid`,p+4);value.write('synthetic-user',p+54);value.write('synthetic-password',p+104);
  }
  return value;
 }
 function parse(value, length=316) { memory.set(value,start); return api.ncc_apn_test_parse(length); }
 function reject(value,length=316) { assert.equal(parse(value,length),0);assert.equal(api.ncc_apn_output_zero(),1); }
 assert.equal(parse(fixture()),1);assert.equal(api.ncc_apn_test_auth(1),1);
 for(const size of [0,315,317])reject(fixture(),size);
 for(const [offset,value] of [[0,0],[8,2],[162,1],[9,4],[10,1],[11,1]]) { const f=fixture();f[offset]=value;reject(f); }
 for(const field of [12,62,112,166,216,266]) {
  for(const character of [10,13,31,127,128,34,92]) { const f=fixture();f[field]=character;reject(f); }
  const full=fixture();full.fill(65,field,field+50);reject(full);
  const trailing=fixture();trailing[field]=0;trailing[field+1]=65;reject(trailing);
  const longest=fixture();longest.fill(65,field,field+49);longest[field+49]=0;assert.equal(parse(longest),1);
 }
 const empty=fixture();empty.fill(0,12,62);reject(empty);
 const noAuth=fixture();for(const p of [8,162]) {noAuth[p+1]=0;noAuth.fill(0,p+54,p+154);}assert.equal(parse(noAuth),1);
 console.log('PASS: compiled C private APN framing, two-profile order, IPv4/auth guards, AT escaping rejection, zero-on-failure');
})().catch(error=>{console.error(error);process.exitCode=1;});
