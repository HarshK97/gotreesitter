package grammars

import (
	"crypto/sha256"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

func TestBuiltinExternalScannerRetryProfilesAttach(t *testing.T) {
	t.Cleanup(func() { PurgeEmbeddedLanguageCache() })

	tests := []struct {
		name string
		load func() *gotreesitter.Language
	}{
		{name: "dart", load: DartLanguage},
		{name: "kotlin", load: KotlinLanguage},
		{name: "python", load: PythonLanguage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lang := tt.load()
			if lang.ExternalScanner == nil {
				t.Fatal("ExternalScanner = nil, want attached scanner")
			}
			if got := lang.ExternalScannerFullParseRetryPolicy; got != gotreesitter.ExternalScannerFullParseRetrySkipRepeat {
				t.Fatalf("ExternalScannerFullParseRetryPolicy = %d, want certified skip-repeat policy", got)
			}
		})
	}
}

func TestBuiltinRuntimeProfilesStayNarrow(t *testing.T) {
	if got, want := len(builtinLanguageRuntimeProfiles), 14; got != want {
		t.Fatalf("builtinLanguageRuntimeProfiles has %d entries, want %d", got, want)
	}
	lang := &gotreesitter.Language{ExternalScanner: KotlinExternalScanner{}}
	if attachBuiltinLanguageRuntimeProfile("ruby", [32]byte{}, lang) {
		t.Fatal("unknown runtime profile reported an attachment")
	}
	if got := lang.ExternalScannerFullParseRetryPolicy; got != gotreesitter.ExternalScannerFullParseRetryDefault {
		t.Fatalf("unknown runtime profile changed policy to %d", got)
	}
	if got := lang.FullParseAcceptedErrorRetryProfile; got != (gotreesitter.FullParseAcceptedErrorRetryProfile{}) {
		t.Fatalf("unknown runtime profile changed accepted-error retry profile to %+v", got)
	}
}

func TestBuiltinHaskellConflictPolicyAttaches(t *testing.T) {
	PurgeEmbeddedLanguageCache()
	t.Cleanup(func() { PurgeEmbeddedLanguageCache() })

	lang := HaskellLanguage()
	for _, policy := range lang.ConflictPolicies {
		if policy.State != 11192 || policy.Lookahead != 4 {
			continue
		}
		if policy.Kind != gotreesitter.ConflictPolicyRepetitionReduce ||
			len(policy.ReduceSymbols) != 1 || policy.ReduceSymbols[0] != 500 {
			t.Fatalf("Haskell conflict policy = %+v, want certified expression-list reduce", policy)
		}
		return
	}
	t.Fatal("Haskell expression-list conflict policy was not attached")
}

func TestBuiltinHaskellConflictPolicyRequiresCertifiedBlobAndAttachesOnce(t *testing.T) {
	lang := &gotreesitter.Language{Name: "haskell"}
	if attachBuiltinLanguageRuntimeProfile("haskell", sha256.Sum256([]byte("uncertified")), lang) {
		t.Fatal("uncertified Haskell blob reported a runtime-profile attachment")
	}
	if len(lang.ConflictPolicies) != 0 {
		t.Fatalf("uncertified Haskell blob attached %d conflict policies", len(lang.ConflictPolicies))
	}

	blob := BlobByName("haskell")
	if len(blob) == 0 {
		t.Fatal("BlobByName(haskell) returned no data")
	}
	sum := sha256.Sum256(blob)
	if !attachBuiltinLanguageRuntimeProfile("haskell", sum, lang) {
		t.Fatal("certified Haskell blob did not attach its runtime profile")
	}
	if got := len(lang.ConflictPolicies); got != 1 {
		t.Fatalf("certified Haskell conflict policies = %d, want 1", got)
	}
	if attachBuiltinLanguageRuntimeProfile("haskell", sum, lang) {
		t.Fatal("reattaching the same Haskell profile reported a change")
	}
	if got := len(lang.ConflictPolicies); got != 1 {
		t.Fatalf("reattached Haskell conflict policies = %d, want 1", got)
	}
}

func TestBuiltinCompleteAcceptedErrorRetryProfilesAttach(t *testing.T) {
	PurgeEmbeddedLanguageCache()
	t.Cleanup(func() { PurgeEmbeddedLanguageCache() })

	tests := []struct {
		name string
		load func() *gotreesitter.Language
	}{
		{name: "bash", load: BashLanguage},
		{name: "caddy", load: CaddyLanguage},
		{name: "cpp", load: CppLanguage},
		{name: "haxe", load: HaxeLanguage},
		{name: "kdl", load: KdlLanguage},
		{name: "odin", load: OdinLanguage},
		{name: "rego", load: RegoLanguage},
		{name: "scss", load: ScssLanguage},
		{name: "tcl", load: TclLanguage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := tt.load().FullParseAcceptedErrorRetryProfile
			if !profile.SkipCompleteAcceptedErrorRetry {
				t.Fatalf("FullParseAcceptedErrorRetryProfile = %+v, want skip-complete certification", profile)
			}
			if tt.name == "tcl" && profile.FreshErrorNoStacksMaxPasses != 1 {
				t.Fatalf("FullParseAcceptedErrorRetryProfile = %+v, want one fresh no-stacks retry", profile)
			}
		})
	}
}

func TestBuiltinJavaAcceptedErrorRetryProfileAttaches(t *testing.T) {
	PurgeEmbeddedLanguageCache()
	t.Cleanup(func() { PurgeEmbeddedLanguageCache() })

	lang := JavaLanguage()
	if lang.ExternalScanner != nil {
		t.Fatal("Java ExternalScanner != nil; profile must not depend on scanner attachment")
	}
	want := gotreesitter.FullParseAcceptedErrorRetryProfile{
		MinSourceBytes:      64 * 1024,
		InitialStackCeiling: 14,
	}
	if got := lang.FullParseAcceptedErrorRetryProfile; got != want {
		t.Fatalf("FullParseAcceptedErrorRetryProfile = %+v, want %+v", got, want)
	}
}

func TestBuiltinExternalScannerRetryProfilesRequireCertifiedBlob(t *testing.T) {
	lang := &gotreesitter.Language{ExternalScanner: KotlinExternalScanner{}}
	if attachBuiltinLanguageRuntimeProfile("kotlin", sha256.Sum256([]byte("uncertified")), lang) {
		t.Fatal("uncertified Kotlin blob reported a runtime-profile attachment")
	}
	if got := lang.ExternalScannerFullParseRetryPolicy; got != gotreesitter.ExternalScannerFullParseRetryDefault {
		t.Fatalf("uncertified Kotlin blob changed policy to %d", got)
	}

	blob := BlobByName("kotlin")
	if len(blob) == 0 {
		t.Fatal("BlobByName(kotlin) returned no data")
	}
	if !attachBuiltinLanguageRuntimeProfile("kotlin", sha256.Sum256(blob), lang) {
		t.Fatal("certified Kotlin blob did not attach its runtime profile")
	}
	if got := lang.ExternalScannerFullParseRetryPolicy; got != gotreesitter.ExternalScannerFullParseRetrySkipRepeat {
		t.Fatalf("certified Kotlin blob policy = %d, want skip-repeat", got)
	}
}

func TestBuiltinJavaAcceptedErrorRetryProfileRequiresCertifiedBlob(t *testing.T) {
	lang := &gotreesitter.Language{Name: "java"}
	if attachBuiltinLanguageRuntimeProfile("java", sha256.Sum256([]byte("uncertified")), lang) {
		t.Fatal("uncertified Java blob reported a runtime-profile attachment")
	}
	if got := lang.FullParseAcceptedErrorRetryProfile; got != (gotreesitter.FullParseAcceptedErrorRetryProfile{}) {
		t.Fatalf("uncertified Java blob changed retry profile to %+v", got)
	}

	blob := BlobByName("java")
	if len(blob) == 0 {
		t.Fatal("BlobByName(java) returned no data")
	}
	if !attachBuiltinLanguageRuntimeProfile("java", sha256.Sum256(blob), lang) {
		t.Fatal("certified Java blob did not attach its runtime profile")
	}
	want := gotreesitter.FullParseAcceptedErrorRetryProfile{
		MinSourceBytes:      64 * 1024,
		InitialStackCeiling: 14,
	}
	if got := lang.FullParseAcceptedErrorRetryProfile; got != want {
		t.Fatalf("certified Java retry profile = %+v, want %+v", got, want)
	}
}

func TestBuiltinCompleteAcceptedErrorRetryProfileRequiresCertifiedBlob(t *testing.T) {
	for _, name := range []string{"caddy", "haxe", "kdl", "odin", "rego", "scss", "tcl"} {
		t.Run(name, func(t *testing.T) {
			lang := &gotreesitter.Language{Name: name}
			if attachBuiltinLanguageRuntimeProfile(name, sha256.Sum256([]byte("uncertified")), lang) {
				t.Fatalf("uncertified %s blob reported a runtime-profile attachment", name)
			}
			if got := lang.FullParseAcceptedErrorRetryProfile; got != (gotreesitter.FullParseAcceptedErrorRetryProfile{}) {
				t.Fatalf("uncertified %s blob changed retry profile to %+v", name, got)
			}

			blob := BlobByName(name)
			if len(blob) == 0 {
				t.Fatalf("BlobByName(%s) returned no data", name)
			}
			if !attachBuiltinLanguageRuntimeProfile(name, sha256.Sum256(blob), lang) {
				t.Fatalf("certified %s blob did not attach its runtime profile", name)
			}
			if !lang.FullParseAcceptedErrorRetryProfile.SkipCompleteAcceptedErrorRetry {
				t.Fatalf("certified %s retry profile = %+v, want skip-complete certification", name, lang.FullParseAcceptedErrorRetryProfile)
			}
		})
	}
}

func TestAttachLanguageSupportDoesNotCertifyWithoutBlobIdentity(t *testing.T) {
	base := KotlinLanguage()
	lang := &gotreesitter.Language{
		Name:            base.Name,
		ExternalSymbols: append([]gotreesitter.Symbol(nil), base.ExternalSymbols...),
		SymbolNames:     append([]string(nil), base.SymbolNames...),
	}
	if !AttachLanguageSupport("kotlin", lang) {
		t.Fatal("AttachLanguageSupport(kotlin) did not attach scanner support")
	}
	if got := lang.ExternalScannerFullParseRetryPolicy; got != gotreesitter.ExternalScannerFullParseRetryDefault {
		t.Fatalf("AttachLanguageSupport certified policy without blob identity: %d", got)
	}

	java := &gotreesitter.Language{Name: "java"}
	AttachLanguageSupport("java", java)
	if got := java.FullParseAcceptedErrorRetryProfile; got != (gotreesitter.FullParseAcceptedErrorRetryProfile{}) {
		t.Fatalf("AttachLanguageSupport certified Java retry profile without blob identity: %+v", got)
	}

	rego := &gotreesitter.Language{Name: "rego"}
	AttachLanguageSupport("rego", rego)
	if got := rego.FullParseAcceptedErrorRetryProfile; got != (gotreesitter.FullParseAcceptedErrorRetryProfile{}) {
		t.Fatalf("AttachLanguageSupport certified Rego retry profile without blob identity: %+v", got)
	}
}
