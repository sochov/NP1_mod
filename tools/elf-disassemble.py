"""Local, read-only AArch64 disassembly for compatibility investigation.
Optional host dependencies: pyelftools, capstone. No vendor code is executed.
"""
import argparse
from capstone import Cs, CS_ARCH_ARM64, CS_MODE_ARM
from elftools.elf.elffile import ELFFile
parser=argparse.ArgumentParser()
parser.add_argument('file')
parser.add_argument('--address',default='entry')
parser.add_argument('--size',type=int,default=256)
args=parser.parse_args()
with open(args.file,'rb') as source:
    elf=ELFFile(source)
    address=elf['e_entry'] if args.address=='entry' else int(args.address,0)
    imports={}
    plt=elf.get_section_by_name('.plt')
    relocations=elf.get_section_by_name('.rela.plt')
    if plt and relocations:
        symbols=elf.get_section(relocations['sh_link'])
        for index,relocation in enumerate(relocations.iter_relocations()):
            imports[plt['sh_addr']+32+index*16]=symbols.get_symbol(relocation['r_info_sym']).name
    section=next(s for s in elf.iter_sections() if s['sh_addr']<=address<s['sh_addr']+s['sh_size'] and s['sh_type']!='SHT_NOBITS')
    data=section.data()[address-section['sh_addr']:address-section['sh_addr']+args.size]
    for ins in Cs(CS_ARCH_ARM64,CS_MODE_ARM).disasm(data,address):
        hint=''
        if ins.mnemonic in ('bl','b') and ins.op_str.startswith('#'):
            hint=imports.get(int(ins.op_str[1:],0),'')
        print(hex(ins.address),ins.mnemonic,ins.op_str,hint)
