/* Fork of proven 07bcc guard runtime for the separate Alexa trial only.
 * The setting helper source and ELF remain unchanged. */
#include "native-core.h"
#include "vendor-fingerprints.h"
#include "vendor-slots.h"
extern long syscall(long,...);
static void*(*real_memory_alloc)(void*,const unsigned short*,long);
extern void *dlopen(const char*,int);
extern void *dlsym(void*,const char*);
extern char *dlerror(void);
extern int dlclose(void*);
extern char *getenv(const char*);
extern unsigned int geteuid(void);
extern int getpid(void);
extern int *__errno_location(void);
struct ncc_dl_info{const char*file;void*base;const char*symbol;void*symbol_address;};
extern int dladdr(void*,struct ncc_dl_info*);
extern int ftok(const char*,int);
extern int clock_gettime(int,void*);
extern int sigemptyset(void*);
extern int sigaddset(void*,int);
extern int sigprocmask(int,const void*,void*);
struct ncc_time{long seconds;long nanoseconds;};
static int(*real_timedlock)(void*,const struct ncc_time*);
static int(*real_unlock)(void*);
static int(*real_trylock)(void*);
static void*(*real_memory_open)(void*,const unsigned short*);
static long(*real_memory_free)(void*);
static int failing;
static int private_create(const char*,int,void*);
static int private_owner(int,unsigned,unsigned,void*);
static int private_unlink(const char*,void*);
static int private_name(const unsigned short*);
static void private_cleanup(void);
static const char*stage="startup";
static const char*mutex_operation="none";
static int global_semaphores[4];
static void memory_ref_before(const unsigned short*);
static int memory_refs_match(void);
static void release_owned_memories(void){unsigned long pointer;if(real_memory_free)while((pointer=ncc_memory_pop())!=0)real_memory_free((void*)pointer);}

static void release_owned_locks(void){unsigned long pointer;if(real_unlock)while((pointer=ncc_track_pop())!=0)real_unlock((void*)pointer);}

static size_t length(const char*s){size_t n=0;while(s[n])n++;return n;}
static void output(const char*s){syscall(64,1,s,length(s));}
static void fail(const char*s,int code){if(!failing){failing=1;release_owned_locks();release_owned_memories();memory_refs_match();}release_owned_locks();private_cleanup();syscall(64,2,s,length(s));syscall(64,2,"\n",1);syscall(94,code);for(;;){}}
/* Loader diagnostics contain only fixed library/symbol metadata. Bound output,
 * remove controls, and keep it on stderr (the launcher stores stderr privately). */
static void loader_fail(const char*stage){const char*detail=dlerror();syscall(64,2,"loader-detail=",14);if(detail){char clean[384];size_t n=0;while(detail[n]&&n<sizeof(clean)){unsigned char c=(unsigned char)detail[n];clean[n]=(c>=32&&c<=126)?(char)c:'?';n++;}syscall(64,2,clean,n);}else syscall(64,2,"unavailable",11);syscall(64,2,"\n",1);fail(stage,78);}
static void diagnostic_text(const char*s){size_t n=0;while(s&&s[n]&&n<192)n++;if(n)syscall(64,2,s,n);}
static void diagnostic_hex(unsigned long value){char out[18];out[0]='0';out[1]='x';const char*h="0123456789abcdef";for(int i=0;i<16;i++)out[17-i]=h[(value>>(4*i))&15];syscall(64,2,out,sizeof(out));}
static void diagnostic_number(unsigned long value){char out[24];size_t n=0;do{out[n++]=(char)('0'+value%10);value/=10;}while(value&&n<sizeof(out));for(size_t i=0;i<n/2;i++){char c=out[i];out[i]=out[n-1-i];out[n-1-i]=c;}syscall(64,2,out,n);}
/* Raw addresses/owner words are written only to the launcher's private stderr.
 * process_vm_readv reads our own process safely instead of dereferencing a bad pointer. */
static void mutex_diagnostic(void*m,int rc,void*caller){diagnostic_text("mutex-detail stage=");diagnostic_text(stage);diagnostic_text(" operation=");diagnostic_text(mutex_operation);diagnostic_text(" rc=");diagnostic_number((unsigned)rc);diagnostic_text(" pointer=");diagnostic_hex((unsigned long)m);diagnostic_text(" caller=");diagnostic_hex((unsigned long)caller);struct ncc_dl_info info;if(dladdr(caller,&info)){diagnostic_text(" image=");diagnostic_text(info.file);diagnostic_text(" offset=");diagnostic_hex((unsigned long)caller-(unsigned long)info.base);}u32 words[6]={0};struct{void*pointer;size_t size;}local={words,sizeof(words)},remote={m,sizeof(words)};long n=syscall(270,getpid(),&local,1,&remote,1,0);diagnostic_text(" raw-words=");if(n==(long)sizeof(words))for(int i=0;i<6;i++){if(i)diagnostic_text(",");diagnostic_hex(words[i]);}else diagnostic_text("unreadable");diagnostic_text("\n");}
/* Fixed Linux arm64 syscalls; these wrappers do not load a vendor library. */
static int read_file(const char*path,u8*b,size_t max){int fd=(int)syscall(56,-100,path,0x20000,0);if(fd<0)return -1;size_t n=0;while(n<max){long r=syscall(63,fd,b+n,max-n);if(r<0){syscall(57,fd);return -1;}if(!r)break;n+=(size_t)r;}syscall(57,fd);return (int)n;}
static u8 snapshot[8193],again[8193];
static int stock_snapshot(void){int n=read_file("/tmp/victoria/victoria_PMEM_AL_SRV_SET_SETTING_ITEM.map",snapshot,sizeof(snapshot));int m=read_file("/tmp/victoria/victoria_PMEM_AL_SRV_SET_SETTING_ITEM.map",again,sizeof(again));if(n!=m||n<128||!ncc_header_ok(snapshot,(size_t)n)||!ncc_header_ok(again,(size_t)m))return 0;for(int i=128;i<n;i++)if(snapshot[i]!=again[i])return 0;return 1;}
static int stock_value(int id){return (int)le32(snapshot+128+44+4*id);}
static int digest_file(const char*path,const char*expected){int fd=(int)syscall(56,-100,path,0x20000,0);if(fd<0)return 0;struct sha_state s;sha_init(&s);u8 buffer[4096],digest[32];unsigned long total=0;for(;;){long n=syscall(63,fd,buffer,sizeof(buffer));if(n<0||total>64UL*1024*1024){syscall(57,fd);return 0;}if(!n)break;total+=(unsigned long)n;sha_update(&s,buffer,(size_t)n);}syscall(57,fd);sha_final(&s,digest);const char*hex="0123456789abcdef";for(int i=0;i<32;i++)if(expected[2*i]!=hex[digest[i]>>4]||expected[2*i+1]!=hex[digest[i]&15])return 0;return 1;}
static void preflight(void){if(geteuid()!=0)fail("root required",77);for(size_t i=0;i<sizeof(vendor_files)/sizeof(vendor_files[0]);i++)if(!digest_file(vendor_files[i].path,vendor_files[i].hash))fail("vendor fingerprint mismatch or unreadable",78);if(!stock_snapshot())fail("stock settings not initialized/stable/supported",78);if(stock_value(50)<0||stock_value(50)>1||stock_value(51)<0||stock_value(51)>2)fail("stock native enum unsupported",78);const char*flags[]={"/tmp/NSTDLIB_EXECUTING_FLAG","/tmp/NSTDLIB_WAITCLEAN_FLAG","/tmp/NPCOMMONLIB_EXECUTING_FLAG","/tmp/NPCOMMONLIB_EXECWAIT_FLAG"};for(int i=0;i<4;i++){int key=ftok(flags[i],1);if(key==-1)fail("existing vendor runtime flag missing",78);int sem=(int)syscall(190,key,1,0);global_semaphores[i]=sem;int count=sem<0?-1:(int)syscall(191,sem,0,12,0);if(count<=0)fail("existing vendor runtime semaphore is not ready",78);}}
/* Interpose process-locally; never patch stock code. Missing IPC must fail,
 * not enter the vendor's first-client/creation/initialization branch. */
static void open_diagnostic(const char*path,int flags,void*caller){diagnostic_text("open-detail stage=");diagnostic_text(stage);diagnostic_text(" flags=");diagnostic_hex((unsigned)flags);diagnostic_text(" caller=");diagnostic_hex((unsigned long)caller);struct ncc_dl_info info;if(dladdr(caller,&info)){diagnostic_text(" image=");diagnostic_text(info.file);diagnostic_text(" offset=");diagnostic_hex((unsigned long)caller-(unsigned long)info.base);}char clean[192];struct{void*pointer;size_t size;}local={clean,sizeof(clean)},remote={(void*)path,sizeof(clean)};long n=syscall(270,getpid(),&local,1,&remote,1,0);diagnostic_text(" path=");if(n>0){size_t length=0;while(length<(size_t)n&&clean[length]){unsigned char c=(unsigned char)clean[length];if(c<32||c>126)clean[length]='?';length++;}syscall(64,2,clean,length);}else diagnostic_text("unreadable");diagnostic_text("\n");}
/* Alloc probes an existing SDK log map with O_EXCL before taking its EEXIST
 * branch. Emulate only that pinned probe; never issue a creating syscall. */
static int existing_log_probe(const char*path,int flags,void*caller){
 if(flags!=0xc2||!same(path,"/tmp/victoria/victoria_PMEM_AL_LOG_BUF.map"))return 0;
 struct ncc_dl_info info;if(!dladdr(caller,&info)||!same(info.file,"/usr/local/lib/libNStandardLib.so")||(unsigned long)caller-(unsigned long)info.base!=0x3247c)return 0;
 int fd=(int)syscall(56,-100,path,0xa0000,0);if(fd<0)return 0; /* O_RDONLY|O_NOFOLLOW|O_CLOEXEC */
 unsigned long status[16]={0};long rc=syscall(80,fd,status);syscall(57,fd);
 /* Linux arm64 struct stat: st_mode +16, st_size +48. Require the pinned
  * 0x1205c payload plus 128-byte vendor header, not a directory or symlink. */
 if(rc||((le32((const u8*)status+16)&0xf000)!=0x8000)||status[6]!=73948)return 0;
 diagnostic_text("existing-log-map-probe=EEXIST; creation-not-attempted\n");*__errno_location()=17;return 1;
}
int open(const char*path,int flags,...){if(flags&(0x200|0x80)){int created=private_create(path,flags,__builtin_return_address(0));if(created>=0)return created;if(existing_log_probe(path,flags,__builtin_return_address(0)))return -1;open_diagnostic(path,flags,__builtin_return_address(0));fail("blocked vendor truncate/exclusive open",78);}int fd=(int)syscall(56,-100,path,flags&~0x40,0);if(fd<0&&(flags&0x40)){open_diagnostic(path,flags,__builtin_return_address(0));fail("vendor attempted to create missing IPC",78);}return fd;}
int semget(int key,int n,int flags){int id=(int)syscall(190,key,n,flags&~(0x200|0x400));if(id<0)fail("vendor attempted missing semaphore",78);return id;}
int semctl(int id,int number,int command,...){int cmd=command&~0x100;unsigned long argument=0;if(!(cmd==2||(cmd>=11&&cmd<=15)||cmd==18||cmd==19))fail("blocked vendor semaphore mutation",78);if(cmd==2||cmd==13||cmd==18||cmd==19){__builtin_va_list args;__builtin_va_start(args,command);argument=__builtin_va_arg(args,unsigned long);__builtin_va_end(args);}return (int)syscall(191,id,number,command,argument);}
struct ncc_semop{unsigned short number;short change;short flags;};
int semop(int id,struct ncc_semop*operations,size_t count){if(!operations||count==0||count>8)fail("unexpected vendor semaphore operation",78);int nonblocking=0;for(size_t i=0;i<count;i++)if(operations[i].flags&0x800)nonblocking=1;struct ncc_time timeout={0,500000000};long rc=syscall(192,id,operations,count,&timeout);if(rc<0&&!nonblocking)fail("vendor semaphore wait failed/timed out",78);return (int)rc;}
int usleep(unsigned microseconds){if(microseconds>100000)fail("unexpected long vendor sleep blocked",78);struct ncc_time t={(long)(microseconds/1000000),(long)(microseconds%1000000)*1000};return (int)syscall(101,&t,(void*)0);}
int pthread_cond_wait(void*a,void*b){(void)a;(void)b;fail("unexpected vendor receive wait blocked",78);return 1;}
int pthread_cond_timedwait(void*a,void*b,const void*c){(void)a;(void)b;(void)c;fail("unexpected vendor receive wait blocked",78);return 1;}
int chown(const char*p,unsigned u,unsigned g){(void)p;(void)u;(void)g;fail("blocked vendor ownership change",78);return -1;}
int fchown(int fd,unsigned u,unsigned g){return private_owner(fd,u,g,__builtin_return_address(0));}
void block_cleanup(void) __asm__("_Z23RemoveAllTemporaryFilesv");
void block_cleanup(void){fail("blocked vendor global temporary-file cleanup",78);}
/* EnableSetting normally may initialize/load defaults. For this helper, only
 * an already initialized, stable map permits the actual vendor Get/Set body. */
unsigned char require_initialized(void*self) __asm__("_ZN17AL_SettingService13EnableSettingEv");
unsigned char require_initialized(void*self){(void)self;return (unsigned char)stock_snapshot();}
static void *symbol(void*h,const char*name){void*p=dlsym(h,name);if(!p)loader_fail("required pinned vendor symbol unavailable");return p;}
/* All stock shared mutexes are entered through these vendor PLT imports.
 * Use a bounded wait and record successful acquisition before returning to vendor code.
 * HUP/INT/TERM are blocked throughout the vendor phase to close the acquisition/recording gap.
 */
static struct ncc_time deadline(void){struct ncc_time t;if(clock_gettime(0,&t))fail("clock unavailable",78);t.nanoseconds+=500000000;if(t.nanoseconds>=1000000000){t.seconds++;t.nanoseconds-=1000000000;}return t;}
static int acquired(void*m,int rc,void*caller){if(rc==0){if(!ncc_track_acquire((unsigned long)m)){real_unlock(m);fail("vendor mutex tracking capacity exceeded",78);}return 0;}mutex_diagnostic(m,rc,caller);if(rc==130){if(real_unlock)real_unlock(m);fail("vendor mutex owner already dead",78);}fail("vendor mutex wait failed/timed out; owned locks released",78);return rc;}
int pthread_mutex_lock(void*m){mutex_operation="lock";if(!real_timedlock)fail("mutex guard bootstrap unavailable; vendor not loaded",78);struct ncc_time t=deadline();return acquired(m,real_timedlock(m,&t),__builtin_return_address(0));}
int pthread_mutex_timedlock(void*m,const struct ncc_time*requested){mutex_operation="timedlock";if(!real_timedlock)fail("mutex guard bootstrap unavailable",78);struct ncc_time t=deadline();if(requested&&(requested->seconds<t.seconds||(requested->seconds==t.seconds&&requested->nanoseconds<t.nanoseconds)))t=*requested;return acquired(m,real_timedlock(m,&t),__builtin_return_address(0));}
int pthread_mutex_trylock(void*m){mutex_operation="trylock";if(!real_trylock)fail("mutex guard bootstrap unavailable",78);int rc=real_trylock(m);if(rc==16)return rc;return acquired(m,rc,__builtin_return_address(0));}
int pthread_mutex_unlock(void*m){if(!real_unlock)fail("mutex guard bootstrap unavailable",78);int rc=real_unlock(m);if(rc==0)ncc_track_release((unsigned long)m);else fail("vendor mutex unlock failed",78);return rc;}
int pthread_create(void*a,const void*b,void*(*c)(void*),void*d){(void)a;(void)b;(void)c;(void)d;fail("unexpected vendor thread creation blocked",78);return 1;}
int unlink(const char*p){return private_unlink(p,__builtin_return_address(0));}
int rmdir(const char*p){(void)p;fail("vendor rmdir blocked",78);return -1;}
void block_system_error(unsigned a,const char*b,unsigned long c,unsigned long d,unsigned long e,unsigned char f) __asm__("_Z12NSystemErrorjPKcmmmb");
void block_system_error(unsigned a,const char*b,unsigned long c,unsigned long d,unsigned long e,unsigned char f){(void)a;(void)b;(void)c;(void)d;(void)e;(void)f;fail("vendor system-error path blocked; owned locks released",78);}
void block_system_error_w(unsigned a,const unsigned short*b,unsigned long c,unsigned long d,unsigned long e,unsigned char f) __asm__("_Z12NSystemErrorjPKDsmmmb");
void block_system_error_w(unsigned a,const unsigned short*b,unsigned long c,unsigned long d,unsigned long e,unsigned char f){(void)a;(void)b;(void)c;(void)d;(void)e;(void)f;fail("vendor wide system-error path blocked; owned locks released",78);}
static void prepare_vendor_phase(void){void*pth=dlopen("libpthread.so.0",6);if(!pth)loader_fail("startup pthread library is not loaded");real_timedlock=(int(*)(void*,const struct ncc_time*))symbol(pth,"pthread_mutex_timedlock");real_unlock=(int(*)(void*))symbol(pth,"pthread_mutex_unlock");real_trylock=(int(*)(void*))symbol(pth,"pthread_mutex_trylock");unsigned long blocked[16]={0};if(sigemptyset(blocked)||sigaddset(blocked,1)||sigaddset(blocked,2)||sigaddset(blocked,15)||sigprocmask(0,blocked,(void*)0))fail("unable to defer termination during vendor locks",78);}
static struct{char path[192];unsigned long before;}memory_references[256];
static unsigned memory_reference_count;
static int reference_read(const char*path,unsigned long*value){u8 header[128];if(read_file(path,header,sizeof(header))!=128)return 0;*value=(unsigned long)le32(header+112)|((unsigned long)le32(header+116)<<32);return 1;}
static void memory_ref_before(const unsigned short*name){char path[192];const char*prefix="/tmp/victoria/victoria_";size_t n=0;while(prefix[n]){path[n]=prefix[n];n++;}if(!name)fail("shared-memory name missing",78);for(size_t i=0;name[i];i++){unsigned c=name[i];if(n>=sizeof(path)-5||!((c>='A'&&c<='Z')||(c>='a'&&c<='z')||(c>='0'&&c<='9')||c=='_'||c=='-'||c=='.'))fail("shared-memory name outside audit format",78);path[n++]=(char)c;}const char*suffix=".map";for(int i=0;i<5;i++)path[n++]=suffix[i];for(unsigned i=0;i<memory_reference_count;i++)if(same(path,memory_references[i].path))return;if(memory_reference_count==256)fail("reference audit capacity exceeded",78);unsigned long before;if(!reference_read(path,&before))fail("preexisting shared-memory header unavailable",78);unsigned index=memory_reference_count++;for(size_t i=0;i<n;i++)memory_references[index].path[i]=path[i];memory_references[index].before=before;}
static int memory_refs_match(void){int ok=1;for(unsigned i=0;i<memory_reference_count;i++){unsigned long after=0;int readable=reference_read(memory_references[i].path,&after);diagnostic_text("memory-ref path=");diagnostic_text(memory_references[i].path);diagnostic_text(" before=");diagnostic_number(memory_references[i].before);diagnostic_text(" after=");if(readable)diagnostic_number(after);else diagnostic_text("unreadable");diagnostic_text("\n");if(!readable||after!=memory_references[i].before)ok=0;}return ok;}
void *tracked_memory_open(void*self,const unsigned short*name) __asm__("_ZN17NProcSharedMemory4OpenEPKDs");
void *tracked_memory_open(void*self,const unsigned short*name){if(!real_memory_open)fail("unexpected shared memory use in NStandard bootstrap",78);if(!private_name(name))memory_ref_before(name);void*data=real_memory_open(self,name);if(data&&!ncc_memory_acquire((unsigned long)self)){real_memory_free(self);fail("vendor shared-memory tracking capacity exceeded",78);}return data;}
long tracked_memory_free(void*self) __asm__("_ZN17NProcSharedMemory10FreeMemoryEv");
long tracked_memory_free(void*self){ncc_memory_release((unsigned long)self);if(!real_memory_free)fail("shared memory cleanup bootstrap unavailable",78);return real_memory_free(self);}
static void print_value(int id,int value){char line[6]={(char)('0'+id/10),(char)('0'+id%10),'=',(char)('0'+value),'\n',0};output(line);}
