#ifndef NCC_NATIVE_NOTIFICATION_CORE_H
#define NCC_NATIVE_NOTIFICATION_CORE_H
/* Separate target. Preserve the proven 07bcc and cb3a55 validators/binaries. */
#define ncc_validate_request ncc_validate_proven_request
#include "native-core.h"
#undef ncc_validate_request
struct ncc_notification_definition { const char *id; int index; unsigned persistent; };
static const struct ncc_notification_definition ncc_notification_definitions[]={
 {"front-car-start",79,538},{"signal-change",80,539}
};
#define NCC_NOTIFICATION_COUNT (sizeof(ncc_notification_definitions)/sizeof(ncc_notification_definitions[0]))
int ncc_validate_request(const char*id,const char*before,const char*next){if(!id||!before||!next)return 0;for(size_t i=0;i<NCC_NOTIFICATION_COUNT;i++)if(same(id,ncc_notification_definitions[i].id))return small_number(before,2)>=0&&small_number(next,2)>=0?ncc_notification_definitions[i].index:0;return 0;}
unsigned ncc_notification_persistent(int index){for(size_t i=0;i<NCC_NOTIFICATION_COUNT;i++)if(ncc_notification_definitions[i].index==index)return ncc_notification_definitions[i].persistent;return 0;}
/* Only replace the legacy main's maximum=2 argument reads in this target.
 * Unexpected future call-site limits fail closed; original source is unchanged. */
int ncc_notification_apply_number(const char*s,int legacy_max){return legacy_max==2?small_number(s,2):-1;}
int ncc_notification_baseline(int expected,int next,int disk,int restoring){return expected>=0&&expected<=2&&next>=0&&next<=2&&disk>=0&&disk<=2&&(disk==expected||(restoring&&disk==next));}
int ncc_notification_disk_value(const u8*b,size_t length,int index){unsigned wanted=ncc_notification_persistent(index);if(!b||!wanted||length!=704||le32(b)!=0||le32(b+4)!=16||le32(b+12)!=86)return -1;unsigned previous=0;int found=-1;for(size_t offset=16;offset<length;offset+=8){unsigned id=le32(b+offset),value=le32(b+offset+4);if(id<=previous)return -1;previous=id;if(id==wanted){if(value>2)return -1;found=(int)value;}}return found;}
#endif
