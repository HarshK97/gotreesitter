# Bytecode stream specification

The canonical parser bytecode stream specification lives in the GoTreeSitter
Hyphae space:

hypha://m31labs/gotreesitter/object/spec.c4-bytecode-isa.v1

Use that object as the source of truth for the instruction set, deterministic
corridor, generalized-LR fallback, stream layout, decode-back proof, and
measurement gates. This repository file is an index only. It prevents the
runbook and repository from pointing at an untracked design document.

The stage 2 corridor compiler and virtual machine are implemented in
`parsercore_c4_program.go` and `parsercore_c4_vm.go`.

The corridor remains disabled by default. Set `GTS_C4_CORRIDOR=1` to enable
the guarded runtime experiment. `REDUCE_CHAIN` and `REDUCE_SHIFT` each have a
separate experiment gate. The default parser path does not execute them.
