#ifndef NCC_NATIVE_CORE_H
#define NCC_NATIVE_CORE_H
/* Portable, side-effect-free validation shared by the helper and host Wasm tests. */
typedef unsigned char u8;
typedef unsigned int u32;
typedef unsigned long size_t;
static int same(const char*a,const char*b){while(*a&&*a==*b){a++;b++;}return *a==*b;}
static u32 le32(const u8*p){return (u32)p[0]|((u32)p[1]<<8)|((u32)p[2]<<16)|((u32)p[3]<<24);}
int ncc_header_ok(const u8*b,size_t n){if(n!=7296||le32(b)!=7168||le32(b+4))return 0;const u8*p=b+128;return le32(p)==1&&le32(p+4)==1&&le32(p+12)==0&&le32(p+20)==44&&le32(p+24)==86;}
static int small_number(const char*s,int max){if(!s[0]||s[1]||s[0]<'0'||s[0]>'0'+max)return -1;return s[0]-'0';}
int ncc_validate_request(const char*id,const char*before,const char*after){int max,index;if(same(id,"tailgating-detection")){index=50;max=1;}else if(same(id,"tailgating-sensitivity")){index=51;max=2;}else return 0;return small_number(before,max)>=0&&small_number(after,max)>=0?index:0;}
static const u32 sha_k[64]={
0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2};
static u32 rr(u32 n,int r){return (n>>r)|(n<<(32-r));}
struct sha_state{u32 h[8];u8 block[64];size_t count;unsigned long long bytes;};
static void sha_init(struct sha_state*s){u32 initial[8]={0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19};for(int i=0;i<8;i++)s->h[i]=initial[i];s->count=0;s->bytes=0;}
static void sha_block(struct sha_state*s){u32 w[64];for(int i=0;i<16;i++){const u8*p=s->block+4*i;w[i]=((u32)p[0]<<24)|((u32)p[1]<<16)|((u32)p[2]<<8)|p[3];}for(int i=16;i<64;i++){u32 a=w[i-15],b=w[i-2];w[i]=w[i-16]+(rr(a,7)^rr(a,18)^(a>>3))+w[i-7]+(rr(b,17)^rr(b,19)^(b>>10));}u32 a=s->h[0],b=s->h[1],c=s->h[2],d=s->h[3],e=s->h[4],f=s->h[5],g=s->h[6],h=s->h[7];for(int i=0;i<64;i++){u32 t=h+(rr(e,6)^rr(e,11)^rr(e,25))+((e&f)^((~e)&g))+sha_k[i]+w[i];u32 v=(rr(a,2)^rr(a,13)^rr(a,22))+((a&b)^(a&c)^(b&c));h=g;g=f;f=e;e=d+t;d=c;c=b;b=a;a=t+v;}s->h[0]+=a;s->h[1]+=b;s->h[2]+=c;s->h[3]+=d;s->h[4]+=e;s->h[5]+=f;s->h[6]+=g;s->h[7]+=h;}
static void sha_update(struct sha_state*s,const u8*p,size_t n){s->bytes+=n;while(n--){s->block[s->count++]=*p++;if(s->count==64){sha_block(s);s->count=0;}}}
static void sha_final(struct sha_state*s,u8*out){unsigned long long bits=s->bytes*8;s->block[s->count++]=128;if(s->count>56){while(s->count<64)s->block[s->count++]=0;sha_block(s);s->count=0;}while(s->count<56)s->block[s->count++]=0;for(int i=0;i<8;i++)s->block[63-i]=(u8)(bits>>(8*i));sha_block(s);for(int i=0;i<32;i++)out[i]=(u8)(s->h[i/4]>>(24-8*(i%4)));}
void ncc_sha256(const u8*p,size_t n,u8*out){struct sha_state s;sha_init(&s);sha_update(&s,p,n);sha_final(&s,out);}
/* Acquisitions are counted individually, including recursive locks. */
static unsigned long ncc_owned_locks[256];
static unsigned ncc_owned_count;
int ncc_track_acquire(unsigned long pointer){if(!pointer||ncc_owned_count==256)return 0;ncc_owned_locks[ncc_owned_count++]=pointer;return 1;}
int ncc_track_release(unsigned long pointer){for(unsigned i=ncc_owned_count;i>0;i--)if(ncc_owned_locks[i-1]==pointer){for(unsigned j=i;j<ncc_owned_count;j++)ncc_owned_locks[j-1]=ncc_owned_locks[j];ncc_owned_count--;return 1;}return 0;}
unsigned long ncc_track_pop(void){return ncc_owned_count?ncc_owned_locks[--ncc_owned_count]:0;}
/* Active NProcSharedMemory objects, not payload addresses. Re-open replaces a mapping. */
static unsigned long ncc_owned_memory[512];
static unsigned ncc_memory_count;
int ncc_memory_acquire(unsigned long pointer){if(!pointer)return 0;for(unsigned i=0;i<ncc_memory_count;i++)if(ncc_owned_memory[i]==pointer)return 1;if(ncc_memory_count==512)return 0;ncc_owned_memory[ncc_memory_count++]=pointer;return 1;}
int ncc_memory_release(unsigned long pointer){for(unsigned i=0;i<ncc_memory_count;i++)if(ncc_owned_memory[i]==pointer){for(unsigned j=i+1;j<ncc_memory_count;j++)ncc_owned_memory[j-1]=ncc_owned_memory[j];ncc_memory_count--;return 1;}return 0;}
unsigned long ncc_memory_pop(void){return ncc_memory_count?ncc_owned_memory[--ncc_memory_count]:0;}
#endif
