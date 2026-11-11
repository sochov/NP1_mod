"""Read-only AArch64 direct caller index for imported/exported ELF symbols.

Usage: python tools/elf-callers.py resources/PRIVATE_ELF --find SymbolFragment
Only addresses and symbol names are emitted; no binary data is extracted.
"""
import argparse
from capstone import Cs, CS_ARCH_ARM64, CS_MODE_ARM
from elftools.elf.elffile import ELFFile

p=argparse.ArgumentParser()
p.add_argument('file')
p.add_argument('--find')
p.add_argument('--target',type=lambda value:int(value,0))
a=p.parse_args()
if bool(a.find)==(a.target is not None):p.error('Specify exactly one of --find or --target')
with open(a.file,'rb') as stream:
    elf=ELFFile(stream)
    targets={} if a.target is None else {a.target:'fixed-local-target'}
    for name in ('.dynsym','.symtab'):
        section=elf.get_section_by_name(name)
        if section:
            for symbol in section.iter_symbols():
                if a.find and a.find in symbol.name and symbol['st_value']:
                    targets[symbol['st_value']]=symbol.name
    plt=elf.get_section_by_name('.plt')
    relocations=elf.get_section_by_name('.rela.plt')
    if plt and relocations:
        symbols=elf.get_section(relocations['sh_link'])
        for index,relocation in enumerate(relocations.iter_relocations()):
            name=symbols.get_symbol(relocation['r_info_sym']).name
            if a.find and a.find in name:
                targets[plt['sh_addr']+32+index*16]=name
    if not targets:
        raise SystemExit('No matching symbol')
    text=elf.get_section_by_name('.text')
    ranges=[]
    for entry in elf.get_dwarf_info().EH_CFI_entries():
        if hasattr(entry,'header') and 'initial_location' in entry.header:
            start=entry['initial_location'];ranges.append((start,start+entry['address_range']))
    for instruction in Cs(CS_ARCH_ARM64,CS_MODE_ARM).disasm(text.data(),text['sh_addr']):
        if instruction.mnemonic in ('bl','b') and instruction.op_str.startswith('#'):
            target=int(instruction.op_str[1:],0)
            if target in targets:
                start=next((start for start,end in ranges if start<=instruction.address<end),None)
                function='unknown' if start is None else hex(start)
                print(hex(instruction.address), 'function='+function, targets[target])
