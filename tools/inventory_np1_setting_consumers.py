"""Static getter call-site inventory; never execute a vendor ELF or mutate a device.

Host-only dependencies: pyelftools, capstone. The nearby ID is a candidate; the
instruction window and complete function must be checked before claiming semantics.
"""
import argparse
import bisect
import hashlib
import json
import pathlib
import struct

from capstone import Cs, CS_ARCH_ARM64, CS_MODE_ARM
from elftools.elf.elffile import ELFFile


def inventory(path):
    with path.open('rb') as stream:
        elf = ELFFile(stream)
        section = elf.get_section_by_name('.text')
        plt = elf.get_section_by_name('.plt')
        relocations = elf.get_section_by_name('.rela.plt')
        if section is None or plt is None or relocations is None:
            return []
        symbols = elf.get_section(relocations['sh_link'])
        targets = {plt['sh_addr'] + 32 + i * 16: symbols.get_symbol(r['r_info_sym']).name
                   for i, r in enumerate(relocations.iter_relocations())}
        targets = {a: n for a, n in targets.items() if 'AL_SettingService9GetStatus' in n}
        functions = sorted((entry['initial_location'], entry['address_range'])
                           for entry in elf.get_dwarf_info().EH_CFI_entries()
                           if hasattr(entry, 'header') and 'initial_location' in entry.header)
        starts = [start for start, _ in functions]
        data, base = section.data(), section['sh_addr']
        decoder = Cs(CS_ARCH_ARM64, CS_MODE_ARM)
        decoder.detail = True
        results = []
        for offset in range(0, len(data) - 3, 4):
            word = struct.unpack_from('<I', data, offset)[0]
            if word & 0xfc000000 not in (0x14000000, 0x94000000):
                continue
            immediate = word & 0x3ffffff
            if immediate & (1 << 25):
                immediate -= 1 << 26
            address = base + offset
            if address + immediate * 4 not in targets:
                continue
            begin = max(0, offset - 96)
            window = list(decoder.disasm(data[begin:offset], base + begin))
            candidate = None
            for instruction in reversed(window):
                if instruction.mnemonic == 'bl':
                    break
                written = [instruction.reg_name(r) for r in instruction.regs_access()[1]]
                if 'w1' in written or 'x1' in written:
                    if instruction.mnemonic == 'mov' and instruction.op_str.startswith('w1, #'):
                        candidate = int(instruction.op_str.split('#')[1], 0)
                    break
            index = bisect.bisect_right(starts, address) - 1
            results.append({'call': hex(address), 'idCandidate': candidate,
                            'function': hex(functions[index][0]) if index >= 0 else None,
                            'size': functions[index][1] if index >= 0 else None,
                            'window': [hex(i.address) + ' ' + i.mnemonic + ' ' + i.op_str for i in window]})
        return results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binaries', nargs='+', type=pathlib.Path)
    parser.add_argument('--out', type=pathlib.Path, default=pathlib.Path('tmp/native-labels/consumer-sites.json'))
    args = parser.parse_args()
    results = []
    for path in args.binaries:
        if path.stat().st_size > 64 * 1024 * 1024:
            parser.error('ELF exceeds 64 MiB')
        sites = inventory(path)
        results.append({'binary': path.name, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'sites': sites})
        print('{0}: {1} getter/tail-getter sites'.format(path.name, len(sites)))
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(results, indent=2), encoding='utf-8')


if __name__ == '__main__':
    main()
