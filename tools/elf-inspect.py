"""Read-only local ELF metadata/symbol inspection; optional: pip install pyelftools.
Never execute copied vendor binaries on the host.
"""
import argparse
from elftools.elf.elffile import ELFFile
parser = argparse.ArgumentParser()
parser.add_argument('file')
parser.add_argument('--find', default='')
args = parser.parse_args()
with open(args.file, 'rb') as source:
    elf = ELFFile(source)
    print('Machine:', elf['e_machine'])
    dynamic = elf.get_section_by_name('.dynamic')
    if dynamic:
        for tag in dynamic.iter_tags('DT_NEEDED'):
            print('NEEDED', tag.needed)
    symbols = elf.get_section_by_name('.dynsym')
    for symbol in symbols.iter_symbols():
        if args.find.lower() in symbol.name.lower():
            print(hex(symbol['st_value']), symbol['st_size'], symbol['st_shndx'], symbol.name)
