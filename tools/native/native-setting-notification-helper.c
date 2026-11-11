/* Two proven three-value notification selectors, isolated from the stable native helpers. */
#include "native-notification-core.h"
#define small_number ncc_notification_apply_number
#define main ncc_proven_setting_main
#include "native-setting-helper.c"
#undef main
#undef small_number
#ifndef NCC_NATIVE_CORE_ONLY
static void notification_consumers(int index){
 if(index&&index!=79&&index!=80)fail("unknown notification consumer",78);
 if(!digest_file("/usr/local/bin/ImageRecog","02a37de0a0a246bd90612e8f4fea77d20aa9556b18259ffc1f9babd9805b4f47"))fail("ImageRecog notification consumer fingerprint mismatch",78);
}
static int notification_saved(int index){u8 data[705];int n=read_file("/pio_data/SYSTEM/SETTING/Setting.DAT",data,sizeof(data));return n<0?-1:ncc_notification_disk_value(data,(size_t)n,index);}
static void notification_semaphores(void){for(int i=0;i<4;i++){char key[6]={'s','e','m',(char)('0'+i),'=',0};output(key);long count=syscall(191,global_semaphores[i],0,12,0);if(count<0)fail("global semaphore read failed",78);char digits[24];size_t n=0;do{digits[n++]=(char)('0'+count%10);count/=10;}while(count);while(n)syscall(64,1,digits+--n,1);output("\n");}}
int main(int argc,char**argv){
 int readonly=argc==2&&(same(argv[1],"--preflight")||same(argv[1],"--vendor-read"));int index=argc==5&&same(argv[1],"--apply")?ncc_validate_request(argv[2],argv[3],argv[4]):0;
 if(!readonly&&!index)fail("notification helper accepts only front-car-start/signal-change and known native 0..2",64);
 if(!stock_snapshot())fail("existing settings snapshot unavailable",78);notification_consumers(index);
 if(argc==2&&same(argv[1],"--preflight")){
  preflight();output("notification-preflight=ok; vendor-not-loaded\n");for(size_t i=0;i<NCC_NOTIFICATION_COUNT;i++){int id=ncc_notification_definitions[i].index,value=stock_value(id),saved=notification_saved(id);if(value<0||value>2||saved<0)fail("notification enum or saved value unsupported",78);print_value(id,value);output("saved-");print_value(id,saved);}print_value(23,stock_value(23));print_value(50,stock_value(50));print_value(51,stock_value(51));print_value(85,stock_value(85));notification_semaphores();return 0;
 }
 if(index){int expected=small_number(argv[3],2),next=small_number(argv[4],2);const char*restore=getenv("NCC_NOTIFICATION_RESTORE");int restoring=restore&&same(restore,"1");if(stock_value(index)!=expected||!ncc_notification_baseline(expected,next,notification_saved(index),restoring))fail("notification expected memory/disk value conflict before vendor load",75);}
 return ncc_proven_setting_main(argc,argv);
}
#endif
