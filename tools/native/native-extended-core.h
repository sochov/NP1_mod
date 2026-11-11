#ifndef NCC_NATIVE_EXTENDED_CORE_H
#define NCC_NATIVE_EXTENDED_CORE_H
/* Separate target: preserve the default validator and the proven 07bcc ELF. */
#define ncc_validate_request ncc_validate_proven_request
#include "native-core.h"
#undef ncc_validate_request
struct ncc_extended_definition{const char*id;int index;unsigned persistent;};
static const struct ncc_extended_definition ncc_extended_definitions[]={
 {"privacy-mode",8,529},{"driving-support",39,495},{"service-cabin-camera",49,505},
 {"traffic-check",61,519},{"traffic-congestion",63,521},{"traffic-regulation",64,522},
 {"ferry",65,523},{"toll-road",66,524},{"smart-ic",67,525}
};
#define NCC_EXTENDED_COUNT (sizeof(ncc_extended_definitions)/sizeof(ncc_extended_definitions[0]))
int ncc_validate_request(const char*id,const char*before,const char*next){if(!id||!before||!next)return 0;for(size_t i=0;i<NCC_EXTENDED_COUNT;i++)if(same(id,ncc_extended_definitions[i].id))return small_number(before,1)>=0&&small_number(next,1)>=0?ncc_extended_definitions[i].index:0;return 0;}
unsigned ncc_extended_persistent(int index){for(size_t i=0;i<NCC_EXTENDED_COUNT;i++)if(ncc_extended_definitions[i].index==index)return ncc_extended_definitions[i].persistent;return 0;}
int ncc_extended_guard(int index,int initial_setup){return index!=8||initial_setup==1;}
int ncc_extended_baseline(int expected,int next,int disk,int restoring){return expected>=0&&expected<=1&&next>=0&&next<=1&&disk>=0&&disk<=1&&(disk==expected||(restoring&&disk==next));}
int ncc_extended_disk_value(const u8*b,size_t length,int index){unsigned wanted=ncc_extended_persistent(index);if(!b||!wanted||length!=704||le32(b)!=0||le32(b+4)!=16||le32(b+12)!=86)return -1;unsigned previous=0;int found=-1;for(size_t offset=16;offset<length;offset+=8){unsigned id=le32(b+offset),value=le32(b+offset+4);if(id<=previous)return -1;previous=id;if(id==wanted){if(value>1)return -1;found=(int)value;}}return found;}
#endif
