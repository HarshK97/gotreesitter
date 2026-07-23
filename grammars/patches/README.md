# Upstream grammar patches

These patches are narrow, pinned overlays applied by `cmd/ts2go` before it
extracts a grammar table. They exist only when an upstream grammar has a
confirmed correctness gap that must be shipped before its next release.

`tree-sitter-typescript-import-type.patch` adds the TypeScript `import_type`
production required for `foo<typeof import("module")>()` and
`foo<import("module").Name>()`. It applies to the TypeScript commit pinned in
`../languages.lock`; regeneration fails if upstream changes the surrounding
grammar shape. Remove the patch and its C-oracle application once the upstream
release contains an equivalent production, then refresh both TS/TSX blobs and
their parity fixtures.
