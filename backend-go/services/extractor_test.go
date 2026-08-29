package services

import (
	"archive/zip"
	"bytes"
	"fmt"
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

// The packing loop used to `break` when a file did not fit. Files are sorted
// by score, so the first oversized one ended the loop and everything below it
// was lost — including small files that would have fitted in what was left.
//
// The discriminator is lastsmall.go: it scores lowest, so it sorts last, and it
// is only ever reached if packing continues past a file that did not fit.
func TestProcessZipKeepsPackingPastAFileThatDoesNotFit(t *testing.T) {
	files := map[string]string{
		// Entrypoint plus oversized: scores highest, sorts first, and is
		// truncated to MaxFileCharacters when packed.
		"main.go": strings.Repeat("x", MaxTotalCharacters*2),
		// Small and at the root, so it scores lowest and sorts last.
		"lastsmall.go": strings.Repeat("// tail\n", 60),
	}
	// Enough mid-scoring files to exhaust the budget before the loop reaches
	// lastsmall.go. Each truncates to MaxFileCharacters, so ten of them are
	// well past MaxTotalCharacters.
	for i := 0; i < 10; i++ {
		files[fmt.Sprintf("services/svc%02d.go", i)] = strings.Repeat("// service line\n", 800)
	}

	snippets, _, _, err := processZip(buildZip(t, files))
	if err != nil {
		t.Fatalf("processZip: %v", err)
	}

	packed := snippetFiles(snippets)
	if !contains(packed, "lastsmall.go") {
		t.Errorf("packing stopped at the first file that did not fit; "+
			"lastsmall.go should still have been packed into the remaining budget. Got %v", packed)
	}

	total := 0
	for _, snip := range snippets {
		if len(snip.Content) > MaxFileCharacters+64 {
			t.Errorf("file %s exceeded the per-file cap: %d chars", snip.File, len(snip.Content))
		}
		total += len(snip.Content)
	}
	if total > MaxTotalCharacters {
		t.Errorf("total budget exceeded: %d > %d", total, MaxTotalCharacters)
	}
	if len(snippets) < 5 {
		t.Errorf("expected the budget to be filled with several files, got %d: %v", len(snippets), packed)
	}
}

// A member far larger than anything we would read should be skipped, not fail
// the whole analysis: a scored .sql dump used to take the repository with it.
func TestProcessZipSkipsAnOversizedMemberWithoutFailing(t *testing.T) {
	files := map[string]string{
		"db/dump.sql":      strings.Repeat("a", maxSkipMemberBytes+1024),
		"services/real.go": strings.Repeat("// code\n", 60),
	}
	snippets, _, _, err := processZip(buildZip(t, files))
	if err != nil {
		t.Fatalf("an oversized member should be skipped, not fail the analysis: %v", err)
	}
	packed := snippetFiles(snippets)
	if !contains(packed, "services/real.go") {
		t.Errorf("expected the real source file to survive, got %v", packed)
	}
	if contains(packed, "db/dump.sql") {
		t.Errorf("the oversized member should not have been packed, got %v", packed)
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
