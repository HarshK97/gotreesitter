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
