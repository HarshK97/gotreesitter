//go:build gts_parsercorephase0

package gotreesitter_test

import (
	"testing"

	gts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
	"github.com/odvcencio/gotreesitter/internal/benchfixtures"
)

func TestAdmissionCandidateYAMLZeroWidthDepth(t *testing.T) {
	fixtures := []struct {
		name      string
		source    string
		wantRoute bool
	}{
		{name: "github-actions", wantRoute: true, source: `name: Go
on:
  push:
    branches: [main]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Test
        run: go test -v ./...
`},
		{name: "docker-compose", source: `services:
  frontend:
    build:
      context: frontend
      target: development
    ports:
      - 3000:3000
    volumes:
      - ./frontend:/usr/src/app
    depends_on:
      - backend
  backend:
    image: example/backend:latest
    environment:
      NODE_ENV: production
`},
		{name: "kubernetes", source: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  labels:
    app: nginx
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
        - name: nginx
          image: nginx:1.14.2
          ports:
            - containerPort: 80
          resources:
            limits:
              memory: "128Mi"
`},
	}
	lang := grammars.YamlLanguage()
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			source := []byte(fixture.source)
			production := gts.NewParser(lang)
			production.SetAdmissionCandidateRoute(false)
			want, err := production.Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			defer want.Release()
			wantInspection, err := benchfixtures.InspectGoTree(want.RootNode(), lang)
			if err != nil {
				t.Fatal(err)
			}

			candidate := gts.NewParser(lang)
			candidate.SetAdmissionCandidateRoute(true)
			gts.ResetAdmissionCandidateCountersForTest()
			got, err := candidate.Parse(source)
			if err != nil {
				t.Fatal(err)
			}
			defer got.Release()
			routed, fallback := gts.AdmissionCandidateCounters()
			if fixture.wantRoute {
				if routed != 1 || fallback != 0 {
					t.Fatalf("candidate did not route: routed=%d fallback=%d reason=%q", routed, fallback, gts.AdmissionCandidateLastFallbackReason())
				}
			} else if routed != 0 || fallback != 1 {
				t.Fatalf("candidate did not fail closed: routed=%d fallback=%d reason=%q", routed, fallback, gts.AdmissionCandidateLastFallbackReason())
			}
			gotInspection, err := benchfixtures.InspectGoTree(got.RootNode(), lang)
			if err != nil {
				t.Fatal(err)
			}
			if gotInspection.SHA256 != wantInspection.SHA256 {
				t.Fatalf("candidate digest=%s production=%s", gotInspection.SHA256, wantInspection.SHA256)
			}
		})
	}
}
