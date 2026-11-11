/* Three proven six-value selectors, isolated from the stable native helpers. */
#include "native-choice-core.h"
#define small_number ncc_choice_apply_number
#define main ncc_proven_setting_main
#include "native-setting-helper.c"
#undef main
#undef small_number
#ifndef NCC_NATIVE_CORE_ONLY
static void choice_consumers(int index){
 if(!index||index==68)if(!digest_file("/usr/local/bin/Navigation","692f35e7097ec07947a3a8e264a5275696ada5b2224ab36f5a440fe8befbcc21"))fail("Navigation consumer fingerprint mismatch",78);
 if(!index||index==77||index==78){
  if(!digest_file("/usr/local/bin/Voice","33eb4066e9445d17a754dd0bcbfa1a2196cd23914bcf6da77c89ba7ebe9ce643"))fail("Voice consumer fingerprint mismatch",78);
  if(!digest_file("/usr/local/lib/libVoicePlayService.so","2b18fe5c318cf632e70705ceb69c505ba2d7903b62c2f8146154b1ae0dd76b04"))fail("VoicePlay consumer fingerprint mismatch",78);
 }
}
static int choice_saved(int index){u8 data[705];int n=read_file("/pio_data/SYSTEM/SETTING/Setting.DAT",data,sizeof(data));return n<0?-1:ncc_choice_disk_value(data,(size_t)n,index);}
static void choice_semaphores(void){for(int i=0;i<4;i++){char key[6]={'s','e','m',(char)('0'+i),'=',0};output(key);long count=syscall(191,global_semaphores[i],0,12,0);if(count<0)fail("global semaphore read failed",78);char digits[24];size_t n=0;do{digits[n++]=(char)('0'+count%10);count/=10;}while(count);while(n)syscall(64,1,digits+--n,1);output("\n");}}
int main(int argc,char**argv){
 int readonly=argc==2&&(same(argv[1],"--preflight")||same(argv[1],"--vendor-read"));int index=argc==5&&same(argv[1],"--apply")?ncc_validate_request(argv[2],argv[3],argv[4]):0;
 if(!readonly&&!index)fail("choice helper accepts only route-search/daily-voice/support-voice and known native 0..5",64);
 if(!stock_snapshot())fail("existing settings snapshot unavailable",78);choice_consumers(index);
 if(argc==2&&same(argv[1],"--preflight")){
  preflight();output("choice-preflight=ok; vendor-not-loaded\n");for(size_t i=0;i<NCC_CHOICE_COUNT;i++){int id=ncc_choice_definitions[i].index,value=stock_value(id),saved=choice_saved(id);if(value<0||value>5||saved<0)fail("choice enum or saved value unsupported",78);print_value(id,value);output("saved-");print_value(id,saved);}print_value(23,stock_value(23));print_value(50,stock_value(50));print_value(51,stock_value(51));print_value(85,stock_value(85));choice_semaphores();return 0;
 }
 if(index){int expected=small_number(argv[3],5),next=small_number(argv[4],5);const char*restore=getenv("NCC_CHOICE_RESTORE");int restoring=restore&&same(restore,"1");if(stock_value(index)!=expected||!ncc_choice_baseline(expected,next,choice_saved(index),restoring))fail("choice expected memory/disk value conflict before vendor load",75);}
 return ncc_proven_setting_main(argc,argv);
}
#endif
