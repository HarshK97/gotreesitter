//go:build gts_parsercorephase0

package gotreesitter_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

func TestAdmissionCandidateGraphQLRootFieldsDirect(t *testing.T) {
	const (
		fixturePath = "testdata/admission_direct/graphql_root_fields.graphql"
		// This source comes from hasura/graphql-engine at commit
		// d769f172d6ba43ecb0254c6be84bb40498608b3d.
		sourceSHA256 = "12cb6d5c35d2f629cebc8c40738796173d86e93543e50c1138dc7f138c1ff061"
		wantDetail   = "digest a6a0c3dd54db"
	)

	source, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read GraphQL admission fixture: %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(source)); got != sourceSHA256 {
		t.Fatalf("GraphQL admission fixture SHA-256 = %s, want %s", got, sourceSHA256)
	}

	var graphql grammars.LangEntry
	for _, entry := range grammars.AllLanguages() {
		if entry.Name == "graphql" {
			graphql = entry
			break
		}
	}
	if graphql.Name == "" {
		t.Fatal("GraphQL grammar is not registered")
	}
	t.Cleanup(func() { grammars.PurgeEmbeddedLanguageCache() })

	row := runAdmissionScorecardSource(graphql, source)
	if row.status != scorecardPass {
		t.Fatalf("GraphQL RootFields compact route = %s: %s", row.status, row.detail)
	}
	if row.detail != wantDetail {
		t.Fatalf("GraphQL RootFields compact digest = %q, want %q", row.detail, wantDetail)
	}
}
