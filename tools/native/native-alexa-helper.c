/* Independent Alexa owner trial. Do not deploy over the proven setting helper.
 * GET/change go to the stock Voice handler; storage ID85 is never written here. */
#include "native-alexa-runtime.h"
#include "alexa-frame.h"
static char private_path[160];
static unsigned short private_map_name[96];
static int private_enabled,private_created,private_fd=-1;
static unsigned long private_device,private_inode;
static int pinned_caller(void*caller,unsigned long offset){struct ncc_dl_info i;return dladdr(caller,&i)&&same(i.file,"/usr/local/lib/libNStandardLib.so")&&(unsigned long)caller-(unsigned long)i.base==offset;}
static int private_identity(int fd){unsigned long st[16]={0};return private_created&&syscall(80,fd,st)==0&&st[0]==private_device&&st[1]==private_inode&&(le32((u8*)st+16)&0xf000)==0x8000;}
static int private_path_identity(void){int fd=(int)syscall(56,-100,private_path,0xa0000,0);if(fd<0)return 0;int same_file=private_identity(fd);syscall(57,fd);return same_file;}
static void prepare_private_name(void){
 const char*prefix="NMSG_MEM_AIPI_server_";char name[96];size_t n=0;while(prefix[n]){name[n]=prefix[n];n++;}unsigned value=(unsigned)getpid();char digits[16];size_t k=0;do{digits[k++]="0123456789ABCDEF"[value&15];value>>=4;}while(value);while(k)name[n++]=digits[--k];name[n]=0;
 for(size_t i=0;i<=n;i++)private_map_name[i]=(unsigned short)name[i];const char*root="/tmp/victoria/victoria_";size_t p=0;while(root[p]){private_path[p]=root[p];p++;}for(size_t i=0;i<n;i++)private_path[p++]=name[i];const char*end=".map";for(int i=0;i<5;i++)private_path[p++]=end[i];
 int fd=(int)syscall(56,-100,private_path,0xa0000,0);if(fd>=0){syscall(57,fd);fail("own PID receiver path already exists; no overwrite",78);}if(*__errno_location()!=2)fail("own PID receiver path cannot be checked",78);
}
static int private_name(const unsigned short*name){if(!name||!private_map_name[0])return 0;for(size_t i=0;i<96;i++){if(name[i]!=private_map_name[i])return 0;if(!name[i])return 1;}return 0;}
static int private_create(const char*path,int flags,void*caller){
 if(!private_enabled||private_created||flags!=0xc2||!same(path,private_path)||!pinned_caller(caller,0x3247c))return -2;
 int fd=(int)syscall(56,-100,path,0xa00c2,0660);if(fd<0)fail("own receiver exclusive creation failed",78);unsigned long st[16]={0};if(syscall(80,fd,st)||(le32((u8*)st+16)&0xf000)!=0x8000){syscall(57,fd);fail("own receiver file identity unavailable",78);}private_fd=fd;private_device=st[0];private_inode=st[1];private_created=1;return fd;
}
static int private_owner(int fd,unsigned uid,unsigned gid,void*caller){if(fd==private_fd&&private_identity(fd)&&uid==0xffffffffu&&pinned_caller(caller,0x32588))return (int)syscall(55,fd,uid,gid);fail("vendor ownership change outside own receiver blocked",78);return -1;}
int fchmod(int fd,unsigned mode){if(fd==private_fd&&private_identity(fd)&&mode==0660&&pinned_caller(__builtin_return_address(0),0x32494))return (int)syscall(52,fd,mode);fail("vendor chmod outside own receiver blocked",78);return -1;}
int ftruncate(int fd,long size){if(fd==private_fd&&private_identity(fd)&&size==8352&&pinned_caller(__builtin_return_address(0),0x325a0))return (int)syscall(46,fd,size);fail("vendor truncate outside own receiver blocked",78);return -1;}
static int private_unlink(const char*path,void*caller){if(private_created&&same(path,private_path)&&pinned_caller(caller,0x31f44)&&private_path_identity()){int rc=(int)syscall(35,-100,path,0);if(!rc)private_created=0;return rc;}fail("vendor unlink outside own receiver blocked",78);return -1;}
static void private_cleanup(void){if(private_created&&private_path_identity()){if(syscall(35,-100,private_path,0)==0)private_created=0;} }
static void sole_receiver_reference(void){for(unsigned i=0;i<100;i++){u8 header[128];if(read_file(private_path,header,sizeof(header))==128&&ncc_a64(header+112)==1)return;usleep(10000);}fail("receiver peer reference did not return; cleanup requires inspection",78);}
void *tracked_memory_alloc(void*self,const unsigned short*name,long size) __asm__("_ZN17NProcSharedMemory5AllocEPKDsl");
void *tracked_memory_alloc(void*self,const unsigned short*name,long size){if(!real_memory_alloc)fail("Alloc guard bootstrap unavailable",78);if(private_name(name)){if(!private_enabled||private_created||size!=8224)fail("unexpected private receiver allocation",78);}else memory_ref_before(name);void*data=real_memory_alloc(self,name,size);if(data&&!ncc_memory_acquire((unsigned long)self)){real_memory_free(self);fail("mapping tracker capacity exceeded",78);}return data;}

struct small_string{char*pointer;unsigned long size;char local[16];};
typedef void(*common_send_fn)(void*,unsigned,unsigned,unsigned short,unsigned short,const void*);
typedef int(*queue_check_fn)(void*);
typedef int(*queue_receive_fn)(void*,u8*,unsigned,unsigned*,unsigned);
static u8 response[8192];
static unsigned request_nonce,voice_pid;
static unsigned parse_pid(const char*s){unsigned v=0;if(!s||!s[0])return 0;for(size_t i=0;s[i];i++){if(i>=10||s[i]<'0'||s[i]>'9'||v>214748364u)return 0;v=v*10+(unsigned)(s[i]-'0');if(v>2147483647u)return 0;}return v;}
static int exchange(common_send_fn send,queue_check_fn check,queue_receive_fn receive,void*name,void*receiver,int command,int requested,int*accepted){
 struct small_string payload;payload.pointer=payload.local;payload.size=0;payload.local[0]=0;if(command==3&&requested==1){payload.local[0]=8;payload.local[1]=1;payload.local[2]=0;payload.size=2;}
 unsigned nonce=++request_nonce;send(name,0x4e434341,nonce,0xb00,(unsigned short)command,&payload);
 for(unsigned tries=0;tries<200;tries++){
  int available=check(receiver);if(available){unsigned size=0;int rc=receive(receiver,response,sizeof(response),&size,0);if(rc==0){int value=0,result=0;if(ncc_alexa_frame(response,size,voice_pid,nonce,(unsigned)(command|0x8000),&value,&result)){*accepted=result;return value;}}else if(rc!=3)fail("own receiver read failed",78);}
  usleep(10000);
 }
 fail("correlated Voice Alexa response timed out; status unknown",78);return -1;
}
int main(int argc,char**argv){
 int apply=argc==5&&same(argv[1],"--apply");int get=argc==3&&same(argv[1],"--get");int only=argc==2&&same(argv[1],"--preflight");if(!apply&&!get&&!only)fail("usage: --preflight | --get voice-pid | --apply voice-pid expected-wire next-wire",64);
 preflight();if(!digest_file("/usr/local/bin/Voice","33eb4066e9445d17a754dd0bcbfa1a2196cd23914bcf6da77c89ba7ebe9ce643")||stock_value(85)<0||stock_value(85)>1)fail("Voice firmware/settings unsupported",78);
 if(only){output("alexa-preflight=ok; vendor-not-loaded\n");print_value(50,stock_value(50));print_value(51,stock_value(51));print_value(85,stock_value(85));for(int i=0;i<4;i++){char key[6]={'s','e','m',(char)('0'+i),'=',0};output(key);long count=syscall(191,global_semaphores[i],0,12,0);if(count<0)fail("semaphore read-back unavailable",78);char digits[24];size_t n=0;do{digits[n++]=(char)('0'+count%10);count/=10;}while(count);while(n)syscall(64,1,digits+--n,1);output("\n");}return 0;}
 voice_pid=parse_pid(argv[2]);int expected=apply?small_number(argv[3],2):0,next=apply?small_number(argv[4],1):0;if(!voice_pid||(apply&&(expected<0||expected>1||next<0)))fail("known Voice PID and writable ON/OFF enums required",64);
 const char*trial=getenv("NCC_NATIVE_ALEXA_TRIAL");if(!trial||!same(trial,"1"))fail("Alexa vendor trial requires explicit launcher environment",78);prepare_private_name();
 stage="pthread-bootstrap";prepare_vendor_phase();stage="NStandard-constructors";void*std=dlopen("/usr/local/lib/libNStandardLib.so",0x102);if(!std)loader_fail("NStandard load failed");
 real_memory_open=(void*(*)(void*,const unsigned short*))symbol(std,"_ZN17NProcSharedMemory4OpenEPKDs");real_memory_free=(long(*)(void*))symbol(std,"_ZN17NProcSharedMemory10FreeMemoryEv");real_memory_alloc=(void*(*)(void*,const unsigned short*,long))symbol(std,"_ZN17NProcSharedMemory5AllocEPKDsl");
 stage="AplLib-constructors";void*h=dlopen("/usr/local/lib/libAplLib.so",0x102);if(!h)loader_fail("AplLib load failed");
 typedef void(*ctor_fn)(void*);typedef void(*ext_ctor_fn)(void*,int,int);typedef void(*name_set_fn)(void*,const unsigned short*);typedef void*(*common_instance_fn)(void*);typedef int(*connect_fn)(void*,unsigned);
 ext_ctor_fn ext_ctor=(ext_ctor_fn)symbol(h,"_ZN22AIPI_ExternalInterfaceC1Eii");ctor_fn ext_dtor=(ctor_fn)symbol(h,"_ZN22AIPI_ExternalInterfaceD1Ev");ctor_fn initialize=(ctor_fn)symbol(h,"_ZN22AIPI_ExternalInterface10InitializeEv");ctor_fn name_ctor=(ctor_fn)symbol(h,"_ZN7NStringC1Ev");ctor_fn name_dtor=(ctor_fn)symbol(h,"_ZN7NStringD1Ev");name_set_fn name_set=(name_set_fn)symbol(h,"_ZN7NString3SetEPKDs");common_instance_fn instance=(common_instance_fn)symbol(h,"_ZN11AIPI_Common11GetInstanceERK7NString");connect_fn connect=(connect_fn)symbol(h,"_ZN9AIPI_Core7ConnectEj");common_send_fn send=(common_send_fn)symbol(h,"_ZN11AIPI_Common29RequestCommonFreeStyleCommandERK7NStringjjttRKNSt7__cxx1112basic_stringIcSt11char_traitsIcESaIcEEE");queue_check_fn check=(queue_check_fn)symbol(h,"_ZN16NProcMsgReceiver8CheckMsgEv");queue_receive_fn receive=(queue_receive_fn)symbol(h,"_ZN16NProcMsgReceiver10ReceiveMsgEPhjPjj");
 u8*base=(u8*)symbol(h,"_Z12std_lib_initv")-0x1ee28;u8*aipi=(u8*)(void*)send-0x2794c0;
 u8*apl=(u8*)symbol(h,"_ZN17AL_SettingService9GetStatusE13AL_SRV_SET_ID")-0xaa8a0;
 if(*(void**)(apl+NCC_AL_ENABLE_SLOT)!=(void*)require_initialized||*(void**)(base+NCC_STD_LOCK_SLOT)!=(void*)pthread_mutex_lock||*(void**)(base+NCC_STD_UNLOCK_SLOT)!=(void*)pthread_mutex_unlock||*(void**)(base+NCC_STD_TIMEDLOCK_SLOT)!=(void*)pthread_mutex_timedlock||*(void**)(base+NCC_STD_TRYLOCK_SLOT)!=(void*)pthread_mutex_trylock||*(void**)(base+NCC_STD_OPEN_SLOT)!=(void*)tracked_memory_open||*(void**)(base+NCC_STD_FREE_SLOT)!=(void*)tracked_memory_free||*(void**)(base+NCC_STD_CLEAN_SLOT)!=(void*)block_cleanup||*(void**)(base+NCC_STD_SEMOP_SLOT)!=(void*)semop||*(void**)(base+0x6d5f8)!=(void*)tracked_memory_alloc||*(void**)(aipi+0x4e1e88)!=(void*)tracked_memory_alloc||*(void**)(base+0x6cd08)!=(void*)open||*(void**)(base+0x6cbe8)!=(void*)fchown||*(void**)(base+0x6ca90)!=(void*)fchmod||*(void**)(base+0x6d200)!=(void*)ftruncate||*(void**)(base+0x6d0f0)!=(void*)unlink)fail("Alexa process-local guard binding mismatch",78);
 struct{unsigned long object[512];unsigned long canary;}external;external.canary=0x4e4343414c455841UL;unsigned long destination[4]={0};const unsigned short voice_name[]={'V','o','i','c','e',0};
 stage="external-interface-constructor";ext_ctor(external.object,8192,8192);name_ctor(destination);name_set(destination,voice_name);stage="Voice-proxy-connect";void*proxy=instance(destination);if(!proxy||connect(proxy,100)!=0)fail("existing Voice IPC unavailable",78);
 stage="own-receiver-initialize";private_enabled=1;initialize(external.object);private_enabled=0;if(!private_created||external.canary!=0x4e4343414c455841UL)fail("own receiver creation unconfirmed",78);
 struct ncc_time now;if(clock_gettime(1,&now))fail("monotonic clock unavailable",78);request_nonce=(unsigned)now.nanoseconds^(unsigned)getpid();int accepted=0;stage="Alexa-get";int state=exchange(send,check,receive,destination,(u8*)external.object+0x98,4,0,&accepted);
 if(apply){if(state!=expected)fail("Alexa expected value conflict or fixed by linkage",75);stage="Alexa-change";int reply=exchange(send,check,receive,destination,(u8*)external.object+0x98,3,next,&accepted);if(!accepted||reply!=next)fail("Voice rejected Alexa change; no direct storage fallback",78);stage="Alexa-readback";state=exchange(send,check,receive,destination,(u8*)external.object+0x98,4,0,&accepted);if(state!=next)fail("Alexa change read-back mismatch",78);}
 stage="own-receiver-peer-release";sole_receiver_reference();stage="own-receiver-destruction";name_dtor(destination);ext_dtor(external.object);release_owned_memories();if(ncc_owned_count||ncc_memory_count||!memory_refs_match())fail("Alexa cleanup reference mismatch",78);private_cleanup();if(private_created||external.canary!=0x4e4343414c455841UL)fail("own receiver cleanup unconfirmed",78);output("alexa-wire=");char number=(char)('0'+state);syscall(64,1,&number,1);output("\ncleanup=matched; own-receiver-removed\n");syscall(94,0);for(;;){}
}
