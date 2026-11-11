/* Official AppManager start-barrier Notify, with no service or map creation. */
#define main ncc_unused_proven_setting_main
#include "native-setting-helper.c"
#undef main
#include "stock-start-core.h"
struct start_target{const char*id;const char*exe;const char*name;const char*sha;};
static const struct start_target targets[]={
 {"drive-rec","/usr/local/bin/DriveRec","DRIVEREC_PROCESS_START_WAIT","000871dc36581f27ae831d9db3772b553a17cce0d814c52867276ac68ea0cde0"},
 {"image-recog","/usr/local/bin/ImageRecog","IMAGERECOG_PROCESS_START_WAIT","02a37de0a0a246bd90612e8f4fea77d20aa9556b18259ffc1f9babd9805b4f47"},
 {"system","/usr/local/bin/System","SYSTEM_PROCESS_START_WAIT","6ff52824c541d187d1d6234c1f49eae32bf6eed3cb9d4e4744741810152e853a"},
 {"remote-monitoring","/usr/local/bin/RemoteMonitoring","REMOTEMONITORING_PROCESS_START_WAIT","2146e0e0c811747d9f8f5e843290e885dca5bf6c2b70d1b71a5fae647f7e579d"},
 {"virtual-passenger","/usr/local/bin/VirtualPassenger","VIRTUALPASSENGER_PROCESS_START_WAIT","9d722e6bad8bbe512eb2f7fa73c9cfef5030cb97273476514b52fe641f5383cf"}
};
/* No Alloc/Create paths are permitted, even accidentally through a destructor. */
void *start_block_alloc(void*self,const unsigned short*name,long size) __asm__("_ZN17NProcSharedMemory5AllocEPKDsl");
void *start_block_alloc(void*self,const unsigned short*name,long size){(void)self;(void)name;(void)size;fail("barrier helper may not allocate shared memory",78);return 0;}
int ftruncate(int fd,long size){(void)fd;(void)size;fail("barrier helper may not truncate",78);return -1;}
int fchmod(int fd,unsigned mode){(void)fd;(void)mode;fail("barrier helper may not chmod",78);return -1;}
static char process_prefix[48],map_path[192];static unsigned long long process_start;static unsigned long map_dev,map_inode;static u8 data[262145];
static void append(char*out,const char*a,const char*b){size_t i=0;while(a[i]){out[i]=a[i];i++;}size_t j=0;do{out[i++]=b[j];}while(b[j++]);}
static int process_read(const char*suffix){char path[80];append(path,process_prefix,suffix);int n=read_file(path,data,sizeof(data)-1);if(n<0||n==(int)sizeof(data)-1)fail("process metadata unreadable/truncated",75);data[n]=0;return n;}
static unsigned owner_live(const struct start_target*t,const char*invocation){char path[80],exe[160];append(path,process_prefix,"/exe");long n=syscall(78,-100,path,exe,sizeof(exe)-1);if(n<=0||n>=(long)sizeof(exe)-1)fail("owner executable unavailable",75);exe[n]=0;if(!same(exe,t->exe))fail("owner executable mismatch",75);int size=process_read("/stat");unsigned long long at=start_time(data,(size_t)size);if(!at||(process_start&&process_start!=at))fail("owner process lifetime changed",75);process_start=at;size=process_read("/environ");if(!start_invocation(data,(size_t)size,invocation))fail("owner invocation mismatch",75);size=process_read("/status");unsigned threads=start_threads(data,(size_t)size);if(!threads)fail("owner thread count unavailable",75);return threads;}
static int map_header(int initial){int fd=(int)syscall(56,-100,map_path,0xa0000,0);if(fd<0)fail("pre-existing barrier map unavailable",78);unsigned long st[16]={0};u8 header[129];long n=syscall(63,fd,header,sizeof(header));int ok=syscall(80,fd,st)==0&&(le32((u8*)st+16)&0xf000)==0x8000&&st[6]==128&&n==128;if(initial){map_dev=st[0];map_inode=st[1];}else ok=ok&&st[0]==map_dev&&st[1]==map_inode;syscall(57,fd);if(!ok)fail("barrier map identity/type changed",78);return start_header(header,128);}
static int owner_waiting(void){int n=process_read("/maps");unsigned long long base=start_map(data,(size_t)n,map_path,map_inode);if(!base)return 0;n=process_read("/syscall");return start_wait_address(base,start_futex(data,(size_t)n));}
int main(int argc,char**argv){
 if(argc!=5||(!same(argv[1],"--preflight")&&!same(argv[1],"--notify"))||!start_hex_id(argv[4]))fail("usage: --preflight|--notify fixed-service-id pid invocation",64);
 const struct start_target*t=0;for(size_t i=0;i<sizeof(targets)/sizeof(targets[0]);i++)if(same(argv[2],targets[i].id))t=&targets[i];unsigned long long pid=start_number((const u8*)argv[3],length(argv[3]),10);if(!t||!pid||pid>2147483647||argv[3][0]=='0')fail("fixed owner and canonical PID required",64);append(process_prefix,"/proc/",argv[3]);preflight();if(!digest_file(t->exe,t->sha))fail("owner firmware mismatch",78);
 append(map_path,"/tmp/victoria/victoria_",t->name);char temp[192];append(temp,map_path,".map");for(size_t i=0;i<=length(temp);i++)map_path[i]=temp[i];int unnotified=map_header(1);
 unsigned threads=owner_live(t,argv[4]);if(threads>1){if(owner_waiting()||owner_live(t,argv[4])<=1)fail("owner has not left its start barrier",75);output("start-barrier=already-running; owner=matched; threads=multiple; vendor-not-loaded\n");return 0;}
 if(!unnotified||!owner_waiting()||owner_live(t,argv[4])!=1)fail("owner is not waiting at the fixed unnotified start barrier",75);
 if(same(argv[1],"--preflight")){output("start-barrier=waiting; owner=matched; vendor-not-loaded\n");return 0;}
 const char*trial=getenv("NCC_STOCK_START_NOTIFY");if(!trial||!same(trial,"1"))fail("explicit start notification launcher required",78);
 stage="pthread-bootstrap";prepare_vendor_phase();stage="NStandard-constructors";void*h=dlopen("/usr/local/lib/libNStandardLib.so",0x102);if(!h)loader_fail("pinned NStandard load failed");
 real_memory_open=(void*(*)(void*,const unsigned short*))symbol(h,"_ZN17NProcSharedMemory4OpenEPKDs");real_memory_free=(long(*)(void*))symbol(h,"_ZN17NProcSharedMemory10FreeMemoryEv");u8*base=(u8*)symbol(h,"_Z12std_lib_initv")-0x1ee28;
 if(*(void**)(base+NCC_STD_LOCK_SLOT)!=(void*)pthread_mutex_lock||*(void**)(base+NCC_STD_UNLOCK_SLOT)!=(void*)pthread_mutex_unlock||*(void**)(base+NCC_STD_OPEN_SLOT)!=(void*)tracked_memory_open||*(void**)(base+NCC_STD_FREE_SLOT)!=(void*)tracked_memory_free||*(void**)(base+NCC_STD_SEMOP_SLOT)!=(void*)semop||*(void**)(base+NCC_STD_TIMEDLOCK_SLOT)!=(void*)pthread_mutex_timedlock||*(void**)(base+NCC_STD_TRYLOCK_SLOT)!=(void*)pthread_mutex_trylock||*(void**)(base+NCC_STD_CLEAN_SLOT)!=(void*)block_cleanup||*(void**)(base+0x6d5f8)!=(void*)start_block_alloc)fail("start helper process-local guards mismatch",78);
 typedef void(*object_fn)(void*);object_fn ctor=(object_fn)symbol(h,"_ZN17NProcSharedMemoryC1Ev"),dtor=(object_fn)symbol(h,"_ZN17NProcSharedMemoryD1Ev"),notify=(object_fn)symbol(h,"_ZN17NProcSharedMemory6NotifyEv");struct{unsigned long words[8];unsigned long canary;}object={{0},0x4e43435354415254UL};unsigned short name[64];size_t i=0;do{name[i]=(unsigned short)t->name[i];}while(t->name[i++]);
 stage="barrier-open";ctor(object.words);if(!tracked_memory_open(object.words,name))fail("pre-existing barrier Open failed",78);if(object.canary!=0x4e43435354415254UL)fail("barrier object bounds mismatch",78);
 if(!map_header(0)||owner_live(t,argv[4])!=1||!owner_waiting())fail("owner stopped waiting before Notify; no notification sent",75);
 stage="barrier-notify";notify(object.words);stage="barrier-object-destructor";dtor(object.words);release_owned_memories();if(ncc_owned_count||ncc_memory_count||object.canary!=0x4e43435354415254UL)fail("helper owned resources did not return to zero",78);/* Starting peers can legitimately change global reference counts. */memory_refs_match();
 stage="barrier-release-observation";for(unsigned j=0;j<100;j++){if(owner_live(t,argv[4])>1&&!owner_waiting()){output("start-barrier=notified; owner=matched; threads=multiple; cleanup=matched\n");syscall(94,0);for(;;){}}usleep(50000);}fail("Notify sent but owner did not leave barrier within five seconds",78);return 78;
}
