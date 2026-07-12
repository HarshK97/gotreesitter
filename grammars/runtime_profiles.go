package grammars

import (
	"encoding/hex"
	"slices"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// builtinLanguageRuntimeProfile contains narrow runtime decisions certified
// against the checked-in grammar/scanner pair. Keep these policies outside the
// parser core: loading the exact certified blob attaches its profile, while
// caller-constructed and adapted languages retain conservative zero defaults.
type builtinLanguageRuntimeProfile struct {
	blobSHA256                         [32]byte
	externalScannerFullParseRetry      gotreesitter.ExternalScannerFullParseRetryPolicy
	fullParseAcceptedErrorRetryProfile gotreesitter.FullParseAcceptedErrorRetryProfile
	conflictPolicies                   []gotreesitter.ConflictPolicy
}

const (
	csharpAcceptedErrorRetryMaxEntryScratchPeak = 690_365
	csharpFreshErrorNoStacksRetryMaxStacks      = 16
)

var builtinLanguageRuntimeProfiles = map[string]builtinLanguageRuntimeProfile{
	// These scanner-backed grammars have certified the first retry ladder's
	// selected accepted-error tree as authoritative. Repeating the whole ladder
	// does not improve the selected tree and imposes a full additional parse.
	"dart": {
		blobSHA256:                    mustRuntimeProfileSHA256("06bac15a9921a2e6af2810fb37ecb29a358b120e137345b9af5fb5f6c6632f59"),
		externalScannerFullParseRetry: gotreesitter.ExternalScannerFullParseRetrySkipRepeat,
	},
	// C#'s certified low-pressure accepted-error trees are authoritative. A
	// fresh no-stacks parse instead benefits from a bounded cap-16 retry; the
	// generic cap-48 ladder exceeds the large-file memory and time budgets.
	"c_sharp": {
		blobSHA256:                    mustRuntimeProfileSHA256("7ad425e89733339dde94e3c03b762ae478fb453b530493f5d62e1ae7537e1784"),
		externalScannerFullParseRetry: gotreesitter.ExternalScannerFullParseRetrySkipRepeat,
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			SkipCompleteAcceptedErrorRetry:   true,
			SkipCompleteMaxEntryScratchPeak:  csharpAcceptedErrorRetryMaxEntryScratchPeak,
			FreshErrorNoStacksRetryMaxStacks: csharpFreshErrorNoStacksRetryMaxStacks,
		},
	},
	// Haxe's accepted-error retry ladder selects the same tree on every pass.
	// Keep the first accepted result instead of running either retry ladder.
	"haxe": {
		blobSHA256: mustRuntimeProfileSHA256("eb39b273148a394f792b322cd30b5483fd6f8ca915b7e15835de4d6482b5a4a7"),
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			SkipCompleteAcceptedErrorRetry: true,
		},
	},
	"kotlin": {
		blobSHA256:                    mustRuntimeProfileSHA256("643a3e6b60d07846dd972849b612159ff9bf09734b09fb00013229c8593a8c78"),
		externalScannerFullParseRetry: gotreesitter.ExternalScannerFullParseRetrySkipRepeat,
	},
	// Odin's accepted-error retry ladder selects the same tree on every pass.
	// Keep the first accepted result instead of running either retry ladder.
	"odin": {
		blobSHA256: mustRuntimeProfileSHA256("9b376bcbbe677780b9031ae84eee4fb59eb37a14fbe169c7c17d35f2b5b776ed"),
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			SkipCompleteAcceptedErrorRetry: true,
		},
	},
	"python": {
		blobSHA256:                    mustRuntimeProfileSHA256("cde4a67dc6af6e1232dbbd1eab8618478d1d73727020e8a8002542390a452d37"),
		externalScannerFullParseRetry: gotreesitter.ExternalScannerFullParseRetrySkipRepeat,
	},
	// Swift's low-pressure accepted-error parses select the same tree across
	// the retry ladder. High-pressure parses still benefit from the first
	// ladder, while repeating that ladder for the external scanner does not.
	"swift": {
		blobSHA256:                    mustRuntimeProfileSHA256("3837a017a16785dbd8daa9661dbd5688393f2c72cf18b568a7957bd35f2cac6d"),
		externalScannerFullParseRetry: gotreesitter.ExternalScannerFullParseRetrySkipRepeat,
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			SkipCompleteAcceptedErrorRetry:  true,
			SkipCompleteMaxEntryScratchPeak: 3 * 64 * 1024,
		},
	},
	// Large ASM accepted-error parses improve during the same-stack merge
	// retry, but later widened-stack passes do not advance the selected tree.
	// Keep that first retry while avoiding the redundant wider passes.
	"asm": {
		blobSHA256: mustRuntimeProfileSHA256("7001e89cc1c597efce3143c011d39a40855067fb06863b738d2c4d7e595fb71d"),
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			MinSourceBytes:      11 * 1024,
			InitialStackCeiling: 8,
		},
	},
	// These grammars have error-bearing real-corpus witnesses that legitimately
	// reach EOF. Re-running the accepted-error ladder does not improve their
	// selected trees, so the exact certified blobs keep the first result.
	"bash": {
		blobSHA256: mustRuntimeProfileSHA256("a3e898c88f6ad918d4d619dff2a4e74d613bda93c90e4a3f9fb7587c1952f3fb"),
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			SkipCompleteAcceptedErrorRetry: true,
		},
	},
	"caddy": {
		blobSHA256: mustRuntimeProfileSHA256("e1af0dcba90bca6949ac1a2756e1a6db2271061b40570b9a7fa2ada29478f6fa"),
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			SkipCompleteAcceptedErrorRetry: true,
		},
	},
	"cpp": {
		blobSHA256: mustRuntimeProfileSHA256("d351f902c8f2ca85257a9296d3c9991862d57701ac6e9006e386ae173fd35178"),
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			SkipCompleteAcceptedErrorRetry: true,
		},
	},
	"kdl": {
		blobSHA256: mustRuntimeProfileSHA256("ef6d000123c053eddebd200a1cbd44d6df5dcab7c4b3d34ae18acdf2f14989f5"),
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			SkipCompleteAcceptedErrorRetry: true,
		},
	},
	"rego": {
		blobSHA256: mustRuntimeProfileSHA256("b10816c87dc847492fbbc1fd97c5096ed35d7abe69d0cd2ef5dd7e02aabac25c"),
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			SkipCompleteAcceptedErrorRetry: true,
		},
	},
	"scss": {
		blobSHA256: mustRuntimeProfileSHA256("0646d27248a96d865a717a2a020ede70762b8a0542fac32a316b34248af9a50e"),
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			SkipCompleteAcceptedErrorRetry: true,
		},
	},
	// Tcl's complete accepted-error trees and first error-bearing no-stacks
	// retry are authoritative for the certified corpus. Later widened passes
	// repeat the same failure without advancing the selected tree.
	"tcl": {
		blobSHA256: mustRuntimeProfileSHA256("4c331e38860001c18b737f6be508f4b09f230c4b9ff95f4b4d12bdb00c176ad7"),
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			SkipCompleteAcceptedErrorRetry: true,
			FreshErrorNoStacksMaxPasses:    1,
		},
	},
	// Haskell's expression-list repeat has one exact comma row where C folds
	// the reduce/repetition-shift pair deterministically. Retaining both arms
	// grows a new GSS frontier for every list element.
	"haskell": {
		blobSHA256: mustRuntimeProfileSHA256("fcfc8794bca4442ebf5688d88e2397c78a22c8f0b585c4e1b868986cfa52dd09"),
		conflictPolicies: []gotreesitter.ConflictPolicy{
			{
				State:         11192,
				Lookahead:     4,
				Kind:          gotreesitter.ConflictPolicyRepetitionReduce,
				ReduceSymbols: []gotreesitter.Symbol{500},
			},
		},
	},
	// On large accepted-error Java sources, the cap-16 same-stack merge retry
	// remains authoritative; the subsequent cap-64 clean/recovery passes do not
	// improve the selected tree. Keep this bound pinned to the exact built-in
	// blob so overrides and caller-built languages retain the generic ladder.
	"java": {
		blobSHA256: mustRuntimeProfileSHA256("530c7257b13e1ce356edd251cac347b5e41f04f74343473c72f43bf1177ffa9c"),
		fullParseAcceptedErrorRetryProfile: gotreesitter.FullParseAcceptedErrorRetryProfile{
			MinSourceBytes:      64 * 1024,
			InitialStackCeiling: 14,
		},
	},
}

func mustRuntimeProfileSHA256(raw string) (sum [32]byte) {
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != len(sum) {
		panic("invalid built-in runtime-profile SHA-256")
	}
	copy(sum[:], decoded)
	return sum
}

func attachBuiltinLanguageRuntimeProfile(name string, blobSHA256 [32]byte, lang *gotreesitter.Language) bool {
	if lang == nil {
		return false
	}
	profile, ok := builtinLanguageRuntimeProfiles[canonicalLanguageName(name)]
	if !ok || blobSHA256 != profile.blobSHA256 {
		return false
	}
	changed := false
	if profile.externalScannerFullParseRetry != gotreesitter.ExternalScannerFullParseRetryDefault &&
		lang.ExternalScanner != nil &&
		lang.ExternalScannerFullParseRetryPolicy != profile.externalScannerFullParseRetry {
		lang.ExternalScannerFullParseRetryPolicy = profile.externalScannerFullParseRetry
		changed = true
	}
	if profile.fullParseAcceptedErrorRetryProfile != (gotreesitter.FullParseAcceptedErrorRetryProfile{}) &&
		lang.FullParseAcceptedErrorRetryProfile != profile.fullParseAcceptedErrorRetryProfile {
		lang.FullParseAcceptedErrorRetryProfile = profile.fullParseAcceptedErrorRetryProfile
		changed = true
	}
	for _, policy := range profile.conflictPolicies {
		if languageHasConflictPolicy(lang, policy) {
			continue
		}
		policy.ReduceSymbols = append([]gotreesitter.Symbol(nil), policy.ReduceSymbols...)
		lang.ConflictPolicies = append(lang.ConflictPolicies, policy)
		changed = true
	}
	return changed
}

func languageHasConflictPolicy(lang *gotreesitter.Language, want gotreesitter.ConflictPolicy) bool {
	if lang == nil {
		return false
	}
	for _, policy := range lang.ConflictPolicies {
		if policy.State == want.State &&
			policy.Lookahead == want.Lookahead &&
			policy.Kind == want.Kind &&
			slices.Equal(policy.ReduceSymbols, want.ReduceSymbols) {
			return true
		}
	}
	return false
}
