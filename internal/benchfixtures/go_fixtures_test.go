package benchfixtures

import (
	"strings"
	"testing"
)

func TestGoFullParseFixturesLoadAuthenticatedSnapshots(t *testing.T) {
	fixtures, err := LoadGoFullParseFixtures()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(fixtures), 4; got != want {
		t.Fatalf("fixture count=%d want=%d", got, want)
	}
	wantIDs := []string{"query_compile", "rewrite", "language", "grammargen_lr"}
	for i, loaded := range fixtures {
		if loaded.Fixture.ID != wantIDs[i] {
			t.Fatalf("fixture[%d].ID=%q want=%q", i, loaded.Fixture.ID, wantIDs[i])
		}
		if err := loaded.Fixture.VerifySource(loaded.Source); err != nil {
			t.Fatalf("fixture[%d]: %v", i, err)
		}
		if loaded.Fixture.OriginRevision != GoFixtureOriginRevision {
			t.Fatalf("fixture[%d].OriginRevision=%q want=%q", i, loaded.Fixture.OriginRevision, GoFixtureOriginRevision)
		}
	}
}

func TestFixtureAdmissionFailsClosed(t *testing.T) {
	fixture := GoFullParseFixtures()[0]
	source, err := fixture.Load()
	if err != nil {
		t.Fatal(err)
	}

	corruptSource := append([]byte(nil), source...)
	corruptSource[len(corruptSource)/2] ^= 1
	if err := fixture.VerifySource(corruptSource); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("corrupt source error=%v, want sha256 failure", err)
	}

	corruptMetadata := fixture
	corruptMetadata.CompressedSHA256 = strings.Repeat("0", 64)
	if _, err := corruptMetadata.Load(); err == nil || !strings.Contains(err.Error(), "compressed sha256") {
		t.Fatalf("corrupt metadata error=%v, want compressed sha256 failure", err)
	}
	if err := fixture.VerifyDeepTreeDigest("deadbeef"); err == nil || !strings.Contains(err.Error(), "deep tree sha256") {
		t.Fatalf("corrupt deep digest error=%v, want deep tree sha256 failure", err)
	}
}

func TestVerifyGoGrammarIdentityFailsClosed(t *testing.T) {
	if err := VerifyGoGrammarIdentity(GoGrammarCommit, GoGrammarBlobSHA256); err != nil {
		t.Fatalf("verified identity: %v", err)
	}
	for _, tc := range []struct {
		name   string
		commit string
		blob   string
	}{
		{name: "missing", commit: "", blob: ""},
		{name: "wrong commit", commit: "deadbeef", blob: GoGrammarBlobSHA256},
		{name: "wrong blob", commit: GoGrammarCommit, blob: "deadbeef"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := VerifyGoGrammarIdentity(tc.commit, tc.blob); err == nil {
				t.Fatal("identity unexpectedly admitted")
			}
		})
	}
}
