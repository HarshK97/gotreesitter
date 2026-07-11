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
	if got, want := len(builtinLanguageRuntimeProfiles), 4; got != want {
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
}
