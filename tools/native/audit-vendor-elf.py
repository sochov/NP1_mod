"""Read-only ELF constructor/PLT audit. Does not load or execute vendor code.

Requires the same host pyelftools/capstone packages as tools/elf-*.py.
The default output is private; only the summary and hashes belong in public docs.
"""
import argparse
import hashlib
import json
import pathlib
import struct
from capstone import Cs,CS_ARCH_ARM64,CS_MODE_ARM
from elftools.elf.elffile import ELFFile
ROOT=pathlib.Path(__file__).resolve().parents[2]
p=argparse.ArgumentParser();p.add_argument('--materials',type=pathlib.Path,default=ROOT/'resources');p.add_argument('--output',type=pathlib.Path,default=ROOT/'tmp/native-settings/constructor-audit.json');a=p.parse_args()
manifest=json.loads((ROOT/'tools/native/vendor-fingerprints.json').read_text());reports=[]
for item in manifest:
 name=item['path'].rsplit('/',1)[1]
 if name=='Setting':continue # The helper never loads the application executable.
 path=a.materials/name
 if hashlib.sha256(path.read_bytes()).hexdigest()!=item['hash']:raise SystemExit('Unreviewed vendor material: '+name)
 with path.open('rb') as f:
  e=ELFFile(f);array=e.get_section_by_name('.init_array');text=e.get_section_by_name('.text');symbols=e.get_section_by_name('.dynsym');names={s['st_value']:s.name for s in symbols.iter_symbols() if s['st_value']};entries=list(struct.unpack('<'+'Q'*(array['sh_size']//8),array.data()))
  relocation=e.get_section_by_name('.rela.dyn');rs=e.get_section(relocation['sh_link'])
  for r in relocation.iter_relocations():
   if array['sh_addr']<=r['r_offset']<array['sh_addr']+array['sh_size']:
    entries[(r['r_offset']-array['sh_addr'])//8]=r['r_addend'] if r['r_info_type']==1027 else rs.get_symbol(r['r_info_sym'])['st_value']+r['r_addend']
  rel=e.get_section_by_name('.rela.plt');plt=e.get_section_by_name('.plt');sy=e.get_section(rel['sh_link']);imports={plt['sh_addr']+32+i*16:sy.get_symbol(r['r_info_sym']).name for i,r in enumerate(rel.iter_relocations())}
  ranges={x['initial_location']:x['address_range'] for x in e.get_dwarf_info().EH_CFI_entries() if hasattr(x,'header') and 'initial_location' in x.header};code=text.data();base=text['sh_addr'];constructors=[]
  for address in entries:
   size=ranges.get(address,128);calls=[];indirect=[]
   for ins in Cs(CS_ARCH_ARM64,CS_MODE_ARM).disasm(code[address-base:address-base+size],address):
    if ins.mnemonic in ('bl','b') and ins.op_str.startswith('#'):
     target=int(ins.op_str[1:],0)
     if not address<=target<address+size:calls.append({'address':hex(target),'symbol':imports.get(target,names.get(target,''))})
    elif ins.mnemonic in ('blr','br'):indirect.append(hex(ins.address))
   constructors.append({'address':hex(address),'symbol':names.get(address,''),'rangeBytes':size,'directCalls':calls,'indirectBranches':indirect})
  tags=e.get_section_by_name('.dynamic');needed=[t.needed for t in tags.iter_tags('DT_NEEDED')];symbolic=any(t.entry.d_tag=='DT_SYMBOLIC' or(t.entry.d_tag=='DT_FLAGS' and t['d_val']&2) for t in tags.iter_tags())
  if symbolic:raise SystemExit('Interposition not guaranteed for symbolic ELF: '+name)
  reports.append({'name':name,'sha256':item['hash'],'needed':needed,'symbolic':symbolic,'constructors':constructors})
a.output.parent.mkdir(parents=True,exist_ok=True);a.output.write_text(json.dumps(reports,indent=2)+'\n')
print('PASS: pinned ELF constructor/PLT inventory for '+str(len(reports))+' vendor libraries; no symbolic binding')
print('Direct-call inventory does not alone prove indirect-call or platform-library safety; see compatibility notes.')
