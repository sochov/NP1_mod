/* Reuse the proven runtime without changing its source/default output. Only
 * this target replaces the pure allowlist and adds pinned consumer checks. */
#include "native-extended-core.h"
#define main ncc_proven_setting_main
#include "native-setting-helper.c"
#undef main
#ifndef NCC_NATIVE_CORE_ONLY
static void extended_consumers(int index){
 if(!index||index==8)if(!digest_file("/usr/local/bin/ImageRecog","02a37de0a0a246bd90612e8f4fea77d20aa9556b18259ffc1f9babd9805b4f47"))fail("ImageRecog consumer fingerprint mismatch",78);
 if(!index||index==49)if(!digest_file("/usr/local/bin/VirtualPassenger","9d722e6bad8bbe512eb2f7fa73c9cfef5030cb97273476514b52fe641f5383cf"))fail("VirtualPassenger consumer fingerprint mismatch",78);
 if(!index|| (index!=8&&index!=49))if(!digest_file("/usr/local/bin/Navigation","692f35e7097ec07947a3a8e264a5275696ada5b2224ab36f5a440fe8befbcc21"))fail("Navigation consumer fingerprint mismatch",78);
}
static int extended_saved(int index){u8 data[705];int n=read_file("/pio_data/SYSTEM/SETTING/Setting.DAT",data,sizeof(data));return n<0?-1:ncc_extended_disk_value(data,(size_t)n,index);}
static void extended_semaphores(void){for(int i=0;i<4;i++){char key[6]={'s','e','m',(char)('0'+i),'=',0};output(key);long count=syscall(191,global_semaphores[i],0,12,0);if(count<0)fail("global semaphore read failed",78);char digits[24];size_t n=0;do{digits[n++]=(char)('0'+count%10);count/=10;}while(count);while(n)syscall(64,1,digits+--n,1);output("\n");}}
int main(int argc,char**argv){
 int readonly=argc==2&&(same(argv[1],"--preflight")||same(argv[1],"--vendor-read"));int index=argc==5&&same(argv[1],"--apply")?ncc_validate_request(argv[2],argv[3],argv[4]):0;
 if(!readonly&&!index)fail("extended helper accepts only the nine proven binary setting IDs and native 0/1",64);
 if(!stock_snapshot())fail("existing settings snapshot unavailable",78);extended_consumers(index);
 if(argc==2&&same(argv[1],"--preflight")){
  preflight();output("extended-preflight=ok; vendor-not-loaded\n");for(size_t i=0;i<NCC_EXTENDED_COUNT;i++){int id=ncc_extended_definitions[i].index,value=stock_value(id),saved=extended_saved(id);if(value<0||value>1||saved<0)fail("extended enum or saved value unsupported",78);print_value(id,value);output("saved-");print_value(id,saved);}print_value(23,stock_value(23));print_value(50,stock_value(50));print_value(51,stock_value(51));print_value(85,stock_value(85));extended_semaphores();return 0;
 }
 if(index){int expected=small_number(argv[3],1),next=small_number(argv[4],1);const char*restore=getenv("NCC_EXTENDED_RESTORE");int restoring=restore&&same(restore,"1");if(!ncc_extended_guard(index,stock_value(23)))fail("privacy requires completed initial setup; stock key guard preserved",78);if(stock_value(index)!=expected||!ncc_extended_baseline(expected,next,extended_saved(index),restoring))fail("extended expected memory/disk value conflict before vendor load",75);}
 return ncc_proven_setting_main(argc,argv);
}
#endif
