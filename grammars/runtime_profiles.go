package grammars

import (
	"encoding/hex"

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
}

var builtinLanguageRuntimeProfiles = map[string]builtinLanguageRuntimeProfile{
	// These scanner-backed grammars have certified the first retry ladder's
	// selected accepted-error tree as authoritative. Repeating the whole ladder
	// does not improve the selected tree and imposes a full additional parse.
	"dart": {
		blobSHA256:                    mustRuntimeProfileSHA256("06bac15a9921a2e6af2810fb37ecb29a358b120e137345b9af5fb5f6c6632f59"),
		externalScannerFullParseRetry: gotreesitter.ExternalScannerFullParseRetrySkipRepeat,
	},
	"kotlin": {
		blobSHA256:                    mustRuntimeProfileSHA256("643a3e6b60d07846dd972849b612159ff9bf09734b09fb00013229c8593a8c78"),
		externalScannerFullParseRetry: gotreesitter.ExternalScannerFullParseRetrySkipRepeat,
	},
	"python": {
		blobSHA256:                    mustRuntimeProfileSHA256("cde4a67dc6af6e1232dbbd1eab8618478d1d73727020e8a8002542390a452d37"),
		externalScannerFullParseRetry: gotreesitter.ExternalScannerFullParseRetrySkipRepeat,
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
	"cpp": {
		blobSHA256: mustRuntimeProfileSHA256("d351f902c8f2ca85257a9296d3c9991862d57701ac6e9006e386ae173fd35178"),
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
	return changed
}
