# Releasing gotreesitter

## Cadence

Planned minor releases are cut on Thursdays in `America/Los_Angeles`. A
Thursday without a coherent, green release is skipped; the schedule is a
batching boundary, not a reason to ship unfinished work.

Patch releases may happen outside the cadence for urgent correctness,
security, or packaging regressions. Ordinary maintenance waits for the next
Thursday. Release planning and in-progress campaign notes live in the private
`hypha://m31labs/gotreesitter` space rather than a public release-train issue.

v0.47.0 is the final planned off-cadence minor. The first cadence release is
the next eligible Thursday after v0.47.0.

## Release checklist

1. Freeze a coherent scope. Merge its pull requests and clear the pull-request
   queue; do not release from a feature branch.
2. Move the accumulated changelog entries from `Unreleased` into a dated
   version section. Update the comparison links and the README release status.
3. Require the release commit's hosted CI to be green. Keep correctness,
   parity, race, and performance evidence distinct; do not substitute a broad
   host test sweep for the isolated gates.
4. Fetch `main`, verify the worktree is clean, verify the version tag does not
   already exist, and record the exact release commit SHA.
5. Create an annotated `vX.Y.Z` tag at that SHA, push only that tag, and create
   the GitHub release from the versioned changelog section.
6. Verify the GitHub release and tag resolve to the recorded SHA and that the
   module can be fetched at the new version.
7. Checkpoint the version, SHA, gate results, and any intentionally deferred
   work in Hyphae. Close campaign issues only when the release contains their
   documented acceptance evidence.

Tags are immutable. If a release is wrong, preserve its tag and publish a
follow-up version.
