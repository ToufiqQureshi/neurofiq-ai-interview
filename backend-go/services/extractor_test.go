package services

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// buildZip produces an archive shaped like a GitHub zipball: every path is
// nested under one "owner-repo-sha/" directory.
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create("owner-repo-abc123/" + name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func snippetFiles(snippets []CodeSnippet) []string {
	names := make([]string, 0, len(snippets))
	for _, s := range snippets {
		names = append(names, s.File)
	}
	return names
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// The scorer previously matched only .go/.py/.ts/.js by suffix, which meant a
// React codebase contributed nothing at all: ".tsx" does not end in ".ts".
func TestProcessZipIncludesJSXAndTSX(t *testing.T) {
	body := strings.Repeat("// a line of application code\n", 60)
	zipData := buildZip(t, map[string]string{
		"src/App.tsx":              body,
		"src/components/Card.jsx":  body,
		"src/services/billing.tsx": body,
		"package.json":             `{"name":"web"}`,
	})

	snippets, _, languages, err := processZip(zipData)
	if err != nil {
		t.Fatalf("processZip: %v", err)
	}

	files := snippetFiles(snippets)
	for _, want := range []string{"src/App.tsx", "src/components/Card.jsx", "src/services/billing.tsx"} {
		if !contains(files, want) {
			t.Errorf("expected %s in snippets, got %v", want, files)
		}
	}
	if len(languages) == 0 || languages[0] != "TypeScript" {
		t.Errorf("expected TypeScript to lead the language list, got %v", languages)
	}
}

func TestProcessZipCoversNonWebLanguages(t *testing.T) {
	body := strings.Repeat("// implementation line\n", 60)
	cases := map[string]string{
		"src/main/java/Service.java": body,
		"src/lib.rs":                 body,
		"app/models/user.rb":         body,
		"Sources/Api.swift":          body,
		"internal/Handler.kt":        body,
		"src/Payment.cs":             body,
		"lib/checkout.php":           body,
	}
	snippets, _, languages, err := processZip(buildZip(t, cases))
	if err != nil {
		t.Fatalf("processZip: %v", err)
	}
	if len(snippets) != len(cases) {
		t.Errorf("expected all %d source files, got %d: %v", len(cases), len(snippets), snippetFiles(snippets))
	}
	if len(languages) < 5 {
		t.Errorf("expected several detected languages, got %v", languages)
	}
}

// A single file bigger than the whole budget used to `break` the packing loop.
// Because files are sorted by score, that meant the highest-scoring file
// emptied the prompt instead of filling it.
func TestProcessZipOneHugeFileDoesNotStarveTheRest(t *testing.T) {
	huge := strings.Repeat("x", MaxTotalCharacters*2)
	small := strings.Repeat("// small service file\n", 50)

	zipData := buildZip(t, map[string]string{
		"main.go":               huge, // scores highest, sorts first
		"services/billing.go":   small,
		"services/accounts.go":  small,
		"controllers/http.go":   small,
		"internal/scheduler.go": small,
	})

	snippets, _, _, err := processZip(zipData)
	if err != nil {
		t.Fatalf("processZip: %v", err)
	}
	if len(snippets) < 4 {
		t.Fatalf("expected the smaller files to still be packed, got %d: %v", len(snippets), snippetFiles(snippets))
	}

	total := 0
	for _, s := range snippets {
		if len(s.Content) > MaxFileCharacters+64 {
			t.Errorf("file %s exceeded the per-file cap: %d chars", s.File, len(s.Content))
		}
		total += len(s.Content)
	}
	if total > MaxTotalCharacters {
		t.Errorf("total budget exceeded: %d > %d", total, MaxTotalCharacters)
	}
}

func TestProcessZipSkipsDependenciesAndLockfiles(t *testing.T) {
	body := strings.Repeat("// code\n", 60)
	zipData := buildZip(t, map[string]string{
		"node_modules/left-pad/index.js": body,
		"vendor/github.com/x/y/z.go":     body,
		"dist/bundle.js":                 body,
		"package-lock.json":              `{"lockfileVersion":3}`,
		"services/real.go":               body,
	})

	snippets, tree, _, err := processZip(zipData)
	if err != nil {
		t.Fatalf("processZip: %v", err)
	}
	files := snippetFiles(snippets)
	if !contains(files, "services/real.go") {
		t.Errorf("expected the real source file, got %v", files)
	}
	for _, unwanted := range []string{"node_modules", "vendor/", "dist/", "package-lock.json"} {
		for _, f := range files {
			if strings.Contains(f, unwanted) {
				t.Errorf("dependency path %q leaked into the prompt", f)
			}
		}
		if strings.Contains(tree, unwanted) {
			t.Errorf("dependency path %q leaked into the directory tree", unwanted)
		}
	}
}

func TestProcessZipSkipsBinaryFiles(t *testing.T) {
	zipData := buildZip(t, map[string]string{
		"assets/logo.ts": "\x00\x01\x02binary masquerading as source",
		"services/ok.go": strings.Repeat("// code\n", 60),
	})
	snippets, _, _, err := processZip(zipData)
	if err != nil {
		t.Fatalf("processZip: %v", err)
	}
	for _, f := range snippetFiles(snippets) {
		if f == "assets/logo.ts" {
			t.Error("binary content was sent to the model")
		}
	}
}

func TestRankLanguagesOrdersByShare(t *testing.T) {
	got := rankLanguages(map[string]int{"Go": 9000, "Shell": 40, "TypeScript": 3000})
	if len(got) < 2 || got[0] != "Go" || got[1] != "TypeScript" {
		t.Errorf("expected Go then TypeScript, got %v", got)
	}
}

func TestRankLanguagesNeverEmpty(t *testing.T) {
	if got := rankLanguages(nil); len(got) != 1 || got[0] != "Unknown" {
		t.Errorf("expected [Unknown] for an empty repo, got %v", got)
	}
}
