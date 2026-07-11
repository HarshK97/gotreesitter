package grammars

import (
	"bytes"
	"compress/gzip"
	"encoding/gob"
	"os"
	"path/filepath"
	"testing"
)

func TestFortranLanguageUsesPreferredGrammargenBlobOverride(t *testing.T) {
	PurgeEmbeddedLanguageCache()
	purgePreferredLanguageOverrideCache()
	t.Cleanup(func() {
		PurgeEmbeddedLanguageCache()
		purgePreferredLanguageOverrideCache()
	})

	original, err := os.ReadFile(filepath.Join("grammar_blobs", "fortran.bin"))
	if err != nil {
		t.Fatalf("read checked-in fortran blob: %v", err)
	}
	lang, err := decodeLanguageBlobData("grammar_blobs/fortran.bin", original)
	if err != nil {
		t.Fatalf("decode checked-in fortran blob: %v", err)
	}
	lang.Name = "fortran-override"

	overrideDir := t.TempDir()
	overridePath := filepath.Join(overrideDir, "fortran.bin")
	if err := os.WriteFile(overridePath, encodeLanguageBlobForTest(t, lang), 0o644); err != nil {
		t.Fatalf("write override blob: %v", err)
	}
	t.Setenv(grammargenBlobDirEnv, overrideDir)

	loaded := FortranLanguage()
	if loaded == nil {
		t.Fatal("FortranLanguage() returned nil with override present")
	}
	if loaded.Name != "fortran-override" {
		t.Fatalf("FortranLanguage().Name = %q, want %q", loaded.Name, "fortran-override")
	}
	if loaded.ExternalScanner == nil {
		t.Fatal("FortranLanguage() override did not receive adapted external scanner")
	}
	if again := FortranLanguage(); again != loaded {
		t.Fatal("FortranLanguage() did not reuse cached override language")
	}
}

func TestFortranLanguageFallsBackWhenPreferredOverrideIsInvalid(t *testing.T) {
	PurgeEmbeddedLanguageCache()
	purgePreferredLanguageOverrideCache()
	t.Cleanup(func() {
		PurgeEmbeddedLanguageCache()
		purgePreferredLanguageOverrideCache()
	})

	overrideDir := t.TempDir()
	overridePath := filepath.Join(overrideDir, "fortran.bin")
	if err := os.WriteFile(overridePath, []byte("not-a-valid-grammar-blob"), 0o644); err != nil {
		t.Fatalf("write invalid override blob: %v", err)
	}
	t.Setenv(grammargenBlobDirEnv, overrideDir)

	loaded := FortranLanguage()
	if loaded == nil {
		t.Fatal("FortranLanguage() returned nil when override blob was invalid")
	}
	if loaded.Name == "fortran-override" {
		t.Fatal("FortranLanguage() should have fallen back to the checked-in blob")
	}
	if loaded.ExternalScanner == nil {
		t.Fatal("FortranLanguage() fallback did not attach external scanner")
	}
}

func TestJsonLanguageUsesPreferredGrammargenBlobOverride(t *testing.T) {
	PurgeEmbeddedLanguageCache()
	purgePreferredLanguageOverrideCache()
	t.Cleanup(func() {
		PurgeEmbeddedLanguageCache()
		purgePreferredLanguageOverrideCache()
	})

	original, err := os.ReadFile(filepath.Join("grammar_blobs", "json.bin"))
	if err != nil {
		t.Fatalf("read checked-in json blob: %v", err)
	}
	lang, err := decodeLanguageBlobData("grammar_blobs/json.bin", original)
	if err != nil {
		t.Fatalf("decode checked-in json blob: %v", err)
	}
	lang.Name = "json-override"

	overrideDir := t.TempDir()
	overridePath := filepath.Join(overrideDir, "json.bin")
	if err := os.WriteFile(overridePath, encodeLanguageBlobForTest(t, lang), 0o644); err != nil {
		t.Fatalf("write override blob: %v", err)
	}
	t.Setenv(grammargenBlobDirEnv, overrideDir)

	loaded := JsonLanguage()
	if loaded == nil {
		t.Fatal("JsonLanguage() returned nil with override present")
	}
	if loaded.Name != "json-override" {
		t.Fatalf("JsonLanguage().Name = %q, want %q", loaded.Name, "json-override")
	}
}

func TestJavaLanguageOverrideDoesNotInheritBuiltinRetryProfile(t *testing.T) {
	PurgeEmbeddedLanguageCache()
	purgePreferredLanguageOverrideCache()
	t.Cleanup(func() {
		PurgeEmbeddedLanguageCache()
		purgePreferredLanguageOverrideCache()
	})

	original, err := os.ReadFile(filepath.Join("grammar_blobs", "java.bin"))
	if err != nil {
		t.Fatalf("read checked-in java blob: %v", err)
	}
	lang, err := decodeLanguageBlobData("grammar_blobs/java.bin", original)
	if err != nil {
		t.Fatalf("decode checked-in java blob: %v", err)
	}
	lang.Name = "java-override"

	overrideDir := t.TempDir()
	overridePath := filepath.Join(overrideDir, "java.bin")
	if err := os.WriteFile(overridePath, encodeLanguageBlobForTest(t, lang), 0o644); err != nil {
		t.Fatalf("write override blob: %v", err)
	}
	t.Setenv(grammargenBlobDirEnv, overrideDir)

	loaded := JavaLanguage()
	if loaded == nil {
		t.Fatal("JavaLanguage() returned nil with override present")
	}
	if loaded.Name != "java-override" {
		t.Fatalf("JavaLanguage().Name = %q, want %q", loaded.Name, "java-override")
	}
	if got := loaded.FullParseAcceptedErrorRetryProfile; got.MinSourceBytes != 0 || got.InitialStackCeiling != 0 {
		t.Fatalf("Java override inherited built-in retry profile: %+v", got)
	}
}

func encodeLanguageBlobForTest(t *testing.T, lang interface{}) []byte {
	t.Helper()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	if err := gob.NewEncoder(gzw).Encode(lang); err != nil {
		_ = gzw.Close()
		t.Fatalf("encode override grammar blob: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("close override grammar gzip writer: %v", err)
	}
	return buf.Bytes()
}
