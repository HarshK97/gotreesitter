# T0 tail cards

This receipt closes the six R1 tail cards from the accepted V10 epoch.
It uses existing evidence only. It changes no parser code and runs no new experiment.

Source epoch: `20260808T202958Z-v10-full-5003ffba`  
Source revision: `5003ffba01e2aee44043e71360c00f5aa93e6e8b`  
Source scoreboard: `harness_out/gcp/20260808T202958Z-v10-full-5003ffba/gts-v10-full/results/scoreboard/scoreboard.json`

R1 counts the Scala 56–58x pair as one card. The table therefore has six cards.

| Card | Witnesses | Ratios | Disposition | First attributable mechanism |
|---:|---|---:|---|---|
| 1 | Python `Lib/test/test_logging.py` | 209.170723x | grouped with card 2 | Initial recovery-node memo; 128 entries and 3,072 bytes |
| 2 | Python `Lib/test/test_socket.py` | 108.118260x | grouped with card 1 | Initial recovery-node memo; 128 entries and 3,072 bytes |
| 3 | Elixir `lib/elixir/lib/enum.ex` | 121.410147x | grouped with card 4 | Temporary recovery-node memo; 262,144 entries and 6,291,456 bytes |
| 4 | Elixir `lib/elixir/lib/macro.ex` | 42.503260x | grouped with card 3 | Temporary recovery-node memo; 262,144 entries and 6,291,456 bytes |
| 5 | Scala `Implicits.scala`, `Namers.scala` | 58.538671x, 55.995092x | grouped | Temporary recovery-node memo; 262,144 entries and 6,291,456 bytes each |
| 6 | Perl `Module/CoreList.pm` | 40.899346x | not-actionable | No mechanism facts; R1 requires a size series |

## Interpretation

The Python, Elixir, and Scala cards form one recovery-memo mechanism cohort.
The evidence supports a grouped recovery candidate, not a parser change.

The Perl card remains not-actionable. Its single 1.2 MB witness cannot separate scale cost from parser, tree, or materialization cost.

No card qualifies as hygiene or fixed-candidate.
Named files remain witnesses. They are not code selectors.

## Evidence boundary

All selected rows have `axes.full.status=ok`, clean full-span trees, and no root error.
The source rows and per-language receipts remain in the accepted V10 artifact.
The signed Hyphae receipt is `hypha-receipt:2026-08-12:codex-t0-tail-cards`.

The next step is T1 mechanism work after B8f and C0f gates close.
