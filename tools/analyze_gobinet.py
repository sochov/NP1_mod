"""Inspect a local AArch64 GobiNet module without loading it or opening a device.

Optional analysis dependencies: pip install pyelftools capstone
Keep the module and detailed output outside Git (for example tmp/).
"""
import argparse
import hashlib
from pathlib import Path

from capstone import Cs, CS_ARCH_ARM64, CS_MODE_LITTLE_ENDIAN
from elftools.elf.elffile import ELFFile


def report(module: Path) -> str:
    lines = [f"sha256={hashlib.sha256(module.read_bytes()).hexdigest()}"]
    with module.open("rb") as stream:
        elf = ELFFile(stream)
        if elf["e_machine"] != "EM_AARCH64":
            raise ValueError("expected an AArch64 module")
        metadata = elf.get_section_by_name(".modinfo")
        if metadata:
            lines += metadata.data().decode(errors="replace").split("\0")
        symbols = elf.get_section_by_name(".symtab")
        if symbols is None:
            raise ValueError("module has no symbol table")
        disassembler = Cs(CS_ARCH_ARM64, CS_MODE_LITTLE_ENDIAN)
        names = {"UserspaceOpen", "UserspaceunlockedIOCTL", "UserspaceRead", "UserspaceWrite", "UserspacePoll", "UserspaceClose", "GetClientID", "ReleaseClientID"}
        functions = {(symbol["st_shndx"], symbol["st_value"]): symbol.name for symbol in symbols.iter_symbols() if symbol["st_info"]["type"] == "STT_FUNC"}
        for symbol in symbols.iter_symbols():
            if symbol.name not in names:
                continue
            section_index = symbol["st_shndx"]
            section = elf.get_section(section_index)
            start, size = symbol["st_value"], symbol["st_size"]
            relocations = {}
            for relocation_section in elf.iter_sections():
                if relocation_section["sh_type"] != "SHT_RELA" or relocation_section["sh_info"] != section_index:
                    continue
                table = elf.get_section(relocation_section["sh_link"])
                for relocation in relocation_section.iter_relocations():
                    offset = relocation["r_offset"]
                    if start <= offset < start + size:
                        target = table.get_symbol(relocation["r_info_sym"])
                        relocations[offset] = f"{target.name or '<section>'}+{relocation['r_addend']:#x} type={relocation['r_info_type']}"
            lines.append(f"\n[{symbol.name}] offset={start:#x} size={size}")
            for instruction in disassembler.disasm(section.data()[start:start + size], start):
                note = f" ; relocation {relocations[instruction.address]}" if instruction.address in relocations else ""
                if instruction.mnemonic in ("bl", "b") and instruction.op_str.startswith("#0x") and not note:
                    target = int(instruction.op_str[1:], 16)
                    if (section_index, target) in functions:
                        note = " ; " + functions[(section_index, target)]
                lines.append(f"{instruction.address:08x}: {instruction.mnemonic:8} {instruction.op_str}{note}")
    return "\n".join(lines)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("module", type=Path)
    parser.add_argument("--output", type=Path, required=True, help="private analysis report path")
    args = parser.parse_args()
    args.output.write_text(report(args.module), encoding="utf-8")
    print(f"Static analysis saved to {args.output}")
