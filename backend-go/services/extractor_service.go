package services

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/config"
	"github.com/ToufiqQureshi/neurofiq-ai-interview/backend-go/models"
)

type CodeSnippet struct {
	File      string `json:"file"`
	LineRange string `json:"line_range"`
	Content   string `json:"content"`
}

type StructureSummary struct {
	DirectoryTree string   `json:"directory_tree"`
	Languages     []string `json:"languages"`
}

// NotableCommit is one line of the repository's history that says something
// about how the code got built. See collectCommitStats for what "notable"
// filters out.
type NotableCommit struct {
	Message string `json:"message"`
	Date    string `json:"date"`
	Author  string `json:"author"`
}

type CommitStats struct {
	TotalCommits   int             `json:"total_commits"`
	Contributors   int             `json:"contributors"`
	FirstCommitAt  string          `json:"first_commit_at"`
	LastCommitAt   string          `json:"last_commit_at"`
	NotableCommits []NotableCommit `json:"notable_commits"`
}

type AnalyzePayload struct {
	RepoFullName     string           `json:"repo_full_name"`
	StructureSummary StructureSummary `json:"structure_summary"`
	CodeSnippets     []CodeSnippet    `json:"code_snippets"`
	CommitStats      CommitStats      `json:"commit_stats"`
}

const (
	// MaxTotalCharacters is the prompt budget for all snippets combined,
	// roughly 15k tokens.
	MaxTotalCharacters = 60000

	// MaxFileCharacters caps any single file's contribution. Without it one
	// large file eats the whole budget and the model sees a single file
	// instead of the shape of the system. The head of a source file is the
	// valuable part — imports, types, and the top-level entry points.
	MaxFileCharacters = 8000

	// maxZipDownloadBytes caps the compressed archive we pull from GitHub.
	// io.ReadAll with no limit means one large monorepo (or a crafted
	// archive) can exhaust the process's memory and take down every
	// connected user with it.
	maxZipDownloadBytes = 120 << 20 // 120 MB

	// maxDecompressedBytes caps what we expand out of that archive. The
	// compressed size is not a safe proxy: a zip bomb is small on the wire
	// and unbounded once decompressed.
	maxDecompressedBytes = 200 << 20 // 200 MB

	// maxFileReadBytes caps a single member of the archive.
	maxFileReadBytes = 2 << 20 // 2 MB
)

// sourceLanguages maps a file extension to the language it represents.
//
// The previous version of this table scored only .go/.py/.ts/.js, which meant
// a Java, Rust, Ruby, C#, Kotlin, Swift, or PHP repository contributed zero
// source files and the interview was generated from package manifests alone.
// Worse, ".tsx" does not end in ".ts" and ".jsx" does not end in ".js", so a
// React codebase — including this product's own frontend — scored nothing.
var sourceLanguages = map[string]string{
	".go": "Go", ".py": "Python", ".rb": "Ruby", ".java": "Java",
	".kt": "Kotlin", ".kts": "Kotlin", ".rs": "Rust", ".swift": "Swift",
	".cs": "C#", ".fs": "F#", ".scala": "Scala", ".clj": "Clojure",
	".ex": "Elixir", ".exs": "Elixir", ".erl": "Erlang", ".hs": "Haskell",
	".php": "PHP", ".pl": "Perl", ".lua": "Lua", ".dart": "Dart",
	".c": "C", ".h": "C", ".cc": "C++", ".cpp": "C++", ".cxx": "C++",
	".hpp": "C++", ".m": "Objective-C", ".mm": "Objective-C++",
	".js": "JavaScript", ".jsx": "JavaScript", ".mjs": "JavaScript",
	".cjs": "JavaScript", ".ts": "TypeScript", ".tsx": "TypeScript",
	".vue": "Vue", ".svelte": "Svelte",
	".sql": "SQL", ".sh": "Shell", ".bash": "Shell", ".ps1": "PowerShell",
	".tf": "Terraform", ".proto": "Protobuf", ".graphql": "GraphQL",
	".gql": "GraphQL", ".r": "R", ".jl": "Julia", ".zig": "Zig",
	".sol": "Solidity", ".nim": "Nim", ".groovy": "Groovy",
}

// manifestFiles are the files that describe how a project is assembled. They
// are the cheapest possible signal about the stack, so they outrank source.
var manifestFiles = map[string]int{
	"package.json": 60, "go.mod": 60, "cargo.toml": 60, "pom.xml": 60,
	"build.gradle": 60, "build.gradle.kts": 60, "requirements.txt": 55,
	"pyproject.toml": 60, "gemfile": 55, "composer.json": 55,
	"dockerfile": 45, "docker-compose.yml": 45, "docker-compose.yaml": 45,
	"makefile": 35, "schema.prisma": 50, "serverless.yml": 40,
	"tsconfig.json": 25, "next.config.js": 30, "vite.config.ts": 30,
}

// entrypointFiles are where a reader starts to understand a codebase.
var entrypointFiles = map[string]int{
	"main.go": 55, "main.py": 55, "main.rs": 55, "app.py": 50,
	"index.ts": 45, "index.js": 45, "app.ts": 45, "app.tsx": 45,
	"server.ts": 50, "server.js": 50, "main.ts": 50, "main.java": 50,
	"program.cs": 50, "application.java": 50, "app.jsx": 45,
}

// architecturalDirs are the directories where design decisions actually live,
// as opposed to generated output or configuration.
var architecturalDirs = []string{
	"service", "services", "controller", "controllers", "handler", "handlers",
	"internal", "pkg", "core", "domain", "usecase", "usecases", "repository",
	"repositories", "middleware", "api", "server", "worker", "workers",
	"model", "models", "entity", "entities", "adapter", "adapters",
	"infra", "infrastructure", "lib", "src/app", "engine",
}

// skipPathFragments are directories and files that are never the candidate's
// own architectural work: dependencies, build output, and vendored code.
var skipPathFragments = []string{
	"node_modules/", ".git/", "vendor/", "dist/", "build/", "target/",
	".next/", ".nuxt/", "__pycache__/", ".venv/", "venv/", "site-packages/",
	".terraform/", "coverage/", ".idea/", ".vscode/", "bower_components/",
	"third_party/", "thirdparty/", "generated/", ".gradle/", "Pods/",
	"migrations/versions/", ".pytest_cache/", "testdata/", "fixtures/",
}

// AnalyzeAndExtract downloads the repo zip, extracts the architecturally
// significant files, and calls the Python worker for the AI analysis.
func AnalyzeAndExtract(userID, repoFullName, token string) (string, error) {
	if !ValidRepoFullName(repoFullName) {
		return "", fmt.Errorf("invalid repository name %q", repoFullName)
	}

	// 1. Repo metadata — one call gives us the default branch and the dates
	//    we need for the history summary.
	meta, err := getRepoMeta(repoFullName, token)
	if err != nil {
		return "", fmt.Errorf("failed to read repo metadata: %w", err)
	}

	// 2. Download the archive, bounded.
	zipData, err := downloadZip(repoFullName, meta.DefaultBranch, token)
	if err != nil {
		return "", fmt.Errorf("failed to download zip: %w", err)
	}

	// 3. Extract and score files.
	snippets, treeStr, languages, err := processZip(zipData)
	if err != nil {
		return "", fmt.Errorf("failed to process zip: %w", err)
	}
	if len(snippets) == 0 {
		return "", fmt.Errorf("no readable source files found in %s", repoFullName)
	}

	// 4. Real commit history. This is the signal no competing product has:
	//    it is what lets a question ask why something was rewritten, not just
	//    what the code currently says. Non-fatal — a repo with an unreadable
	//    history still produces a perfectly good code interview.
	commits := collectCommitStats(repoFullName, token, meta)

	payload := AnalyzePayload{
		RepoFullName: repoFullName,
		StructureSummary: StructureSummary{
			DirectoryTree: treeStr,
			Languages:     languages,
		},
		CodeSnippets: snippets,
		CommitStats:  commits,
	}

	// 5. Call the Python worker for the AI analysis.
	body, err := postToWorker(workerClient, "/internal/analyze-repo", payload)
	if err != nil {
		return "", fmt.Errorf("ai worker failed: %w", err)
	}
	analysisJSON := string(body)

	// 6. Save the result to Postgres.
	//
	// The controller already reserved a "pending" placeholder row for
	// (userID, repoFullName) before starting this background job (see
	// HandleAnalyzeRepo), so this fills that row in rather than inserting a
	// new one — a blind insert here would violate the unique index.
	now := time.Now()
	result := config.DB.Model(&models.GithubProfile{}).
		Where("user_id = ? AND repo_full_name = ?", userID, repoFullName).
		Updates(map[string]interface{}{
			"repo_size_kb":  len(zipData) / 1024,
			"strategy_used": "full_scan",
			"analysis_json": analysisJSON,
			"analyzed_at":   now,
			"expires_at":    now.Add(7 * 24 * time.Hour),
		})
	if result.Error != nil {
		return "", fmt.Errorf("failed to save to db: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return "", fmt.Errorf("no reserved profile row found for %s/%s", userID, repoFullName)
	}

	return analysisJSON, nil
}

type repoMeta struct {
	DefaultBranch string `json:"default_branch"`
	CreatedAt     string `json:"created_at"`
	PushedAt      string `json:"pushed_at"`
	Language      string `json:"language"`
	Size          int    `json:"size"`
}

func getRepoMeta(repoFullName, token string) (repoMeta, error) {
	var meta repoMeta
	body, status, err := githubGet("https://api.github.com/repos/"+repoFullName, token)
	if err != nil {
		return meta, err
	}
	if status != http.StatusOK {
		return meta, fmt.Errorf("github api status %d: %s", status, truncateForError(body))
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return meta, err
	}
	if meta.DefaultBranch == "" {
		meta.DefaultBranch = "main"
	}
	return meta, nil
}

func downloadZip(repoFullName, branch, token string) ([]byte, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/zipball/%s", repoFullName, branch)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api status %d", resp.StatusCode)
	}
	// Bounded read: an unbounded io.ReadAll here is an out-of-memory kill
	// switch that any user can pull by pointing us at a large repository.
	data, err := ReadCapped(resp.Body, maxZipDownloadBytes)
	if err != nil {
		return nil, fmt.Errorf("repository archive too large to analyze (limit %d MB)", maxZipDownloadBytes>>20)
	}
	return data, nil
}

type scoredFile struct {
	name    string
	content string
	score   int
}

// processZip picks the files worth showing the model out of a repository
// archive, and returns them alongside a directory summary and the languages
// actually present.
func processZip(zipData []byte) ([]CodeSnippet, string, []string, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, "", nil, err
	}

	var allFiles []string
	var files []scoredFile
	languageBytes := map[string]int{}
	var decompressed int64

	for _, f := range reader.File {
		// Strip the "owner-repo-sha/" prefix GitHub wraps every zipball in.
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 || parts[1] == "" {
			continue
		}
		name := parts[1]

		if f.FileInfo().IsDir() {
			continue
		}
		if shouldSkipPath(name) {
			continue
		}
		allFiles = append(allFiles, name)

		ext := strings.ToLower(path.Ext(name))
		lang, isSource := sourceLanguages[ext]
		if isSource {
			// Count every source file toward the language mix, even the ones
			// too low-scoring to make the prompt — otherwise "Languages"
			// reports only whatever happened to fit in the budget.
			languageBytes[lang] += int(f.UncompressedSize64)
		}

		score := scoreFile(name, ext, isSource, int64(f.UncompressedSize64))
		if score <= 0 {
			continue
		}

		// Refuse the whole archive rather than expanding a zip bomb: the
		// compressed size we already checked says nothing about this.
		if decompressed+int64(f.UncompressedSize64) > maxDecompressedBytes {
			return nil, "", nil, fmt.Errorf("repository expands beyond the %d MB analysis limit", maxDecompressedBytes>>20)
		}

		rc, err := f.Open()
		if err != nil {
			// One unreadable member is not worth failing the whole analysis.
			log.Printf("extractor: skipping unreadable file %s: %v", name, err)
			continue
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxFileReadBytes))
		rc.Close()
		if err != nil {
			log.Printf("extractor: skipping unreadable file %s: %v", name, err)
			continue
		}
		decompressed += int64(len(content))

		if looksBinary(content) {
			continue
		}
		files = append(files, scoredFile{name: name, content: string(content), score: score})
	}

	// Highest-signal files first, with the name as a tiebreak so the same
	// repository always produces the same prompt (and therefore the same
	// cache behaviour) rather than depending on zip ordering.
	sort.Slice(files, func(i, j int) bool {
		if files[i].score != files[j].score {
			return files[i].score > files[j].score
		}
		return files[i].name < files[j].name
	})

	var snippets []CodeSnippet
	totalChars := 0
	for _, f := range files {
		content := f.content
		lineRange := "all"

		// Truncate rather than drop. The head of a file carries the imports,
		// the types, and the entry points — the parts an architecture
		// question is actually about.
		if len(content) > MaxFileCharacters {
			content = content[:MaxFileCharacters] + "\n// ... (file truncated for analysis)"
			lineRange = fmt.Sprintf("1-%d (truncated)", strings.Count(content, "\n")+1)
		}
		if totalChars+len(content) > MaxTotalCharacters {
			// `continue`, not `break`. Files are sorted by score, so breaking
			// on the first file that does not fit means one large top-scoring
			// file ends the loop and the model sees almost nothing. Skipping
			// it lets the remaining budget fill with smaller files.
			continue
		}
		snippets = append(snippets, CodeSnippet{
			File:      f.name,
			LineRange: lineRange,
			Content:   content,
		})
		totalChars += len(content)
	}

	return snippets, buildTreeSummary(allFiles), rankLanguages(languageBytes), nil
}

// scoreFile rates how much a file tells us about the author's design
// decisions. Zero means "do not send it to the model".
func scoreFile(name, ext string, isSource bool, size int64) int {
	base := path.Base(name)
	lower := strings.ToLower(base)

	score := 0
	if isSource {
		score += 10
	}
	if bonus, ok := manifestFiles[lower]; ok {
		score += bonus
	}
	if bonus, ok := entrypointFiles[lower]; ok {
		score += bonus
	}
	if score == 0 {
		return 0
	}

	// Files under a directory that names a layer are where the interesting
	// decisions live.
	lowerPath := strings.ToLower(name)
	for _, dir := range architecturalDirs {
		if strings.Contains(lowerPath, "/"+dir+"/") || strings.HasPrefix(lowerPath, dir+"/") {
			score += 15
			break
		}
	}

	// Tests describe behaviour rather than design, and generated or minified
	// files describe nothing at all.
	if strings.Contains(lowerPath, "_test.") || strings.Contains(lowerPath, ".test.") ||
		strings.Contains(lowerPath, ".spec.") || strings.HasPrefix(lowerPath, "test/") ||
		strings.Contains(lowerPath, "/test/") || strings.Contains(lowerPath, "/tests/") {
		score -= 8
	}
	if strings.Contains(lower, ".min.") || strings.Contains(lower, ".generated.") ||
		strings.HasSuffix(lower, ".pb.go") || strings.HasSuffix(lower, "_pb2.py") ||
		strings.HasSuffix(lower, ".d.ts") {
		return 0
	}

	// A 40-byte re-export and a 500 KB data file are both noise. Real
	// implementation files sit in between.
	switch {
	case size < 120:
		score -= 8
	case size > 60000:
		score -= 6
	case size >= 800 && size <= 20000:
		score += 6
	}

	// Deeply nested files are usually leaves, not architecture.
	if depth := strings.Count(name, "/"); depth > 5 {
		score -= 4
	}

	if score < 1 {
		return 0
	}
	return score
}

func shouldSkipPath(name string) bool {
	lower := strings.ToLower(name)
	for _, frag := range skipPathFragments {
		if strings.Contains(lower, strings.ToLower(frag)) {
			return true
		}
	}
	// Lockfiles are enormous and say nothing a manifest does not.
	switch path.Base(lower) {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum",
		"cargo.lock", "poetry.lock", "gemfile.lock", "composer.lock":
		return true
	}
	return false
}

// looksBinary rejects files that would waste prompt budget on bytes the model
// cannot read. A NUL byte in the first block is the same heuristic git uses.
func looksBinary(content []byte) bool {
	head := content
	if len(head) > 8000 {
		head = head[:8000]
	}
	return bytes.IndexByte(head, 0) != -1
}

// rankLanguages turns the per-language byte counts into a list ordered by how
// much of the repository each one accounts for. The old code shipped the
// literal string "Auto-detected from files" here, so the model was paying
// tokens for a placeholder.
func rankLanguages(languageBytes map[string]int) []string {
	type langCount struct {
		name  string
		bytes int
	}
	counts := make([]langCount, 0, len(languageBytes))
	total := 0
	for name, b := range languageBytes {
		counts = append(counts, langCount{name, b})
		total += b
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].bytes != counts[j].bytes {
			return counts[i].bytes > counts[j].bytes
		}
		return counts[i].name < counts[j].name
	})

	langs := make([]string, 0, len(counts))
	for _, c := range counts {
		// Drop the long tail: one stray shell script does not make a repo
		// polyglot, and listing it invites questions about nothing.
		if total > 0 && c.bytes*100/total < 2 && len(langs) >= 3 {
			break
		}
		langs = append(langs, c.name)
		if len(langs) == 8 {
			break
		}
	}
	if len(langs) == 0 {
		langs = []string{"Unknown"}
	}
	return langs
}

// buildTreeSummary renders the repository layout as directories with file
// counts rather than a flat list truncated mid-path. A flat dump spent its
// whole budget inside the first alphabetical directory.
func buildTreeSummary(allFiles []string) string {
	const maxChars = 2500

	dirs := map[string][]string{}
	for _, f := range allFiles {
		dir := path.Dir(f)
		if dir == "." {
			dir = "(root)"
		}
		dirs[dir] = append(dirs[dir], path.Base(f))
	}

	names := make([]string, 0, len(dirs))
	for d := range dirs {
		names = append(names, d)
	}
	// Shallow directories first: they describe the shape of the project.
	sort.Slice(names, func(i, j int) bool {
		di, dj := strings.Count(names[i], "/"), strings.Count(names[j], "/")
		if di != dj {
			return di < dj
		}
		return names[i] < names[j]
	})

	var b strings.Builder
	for _, d := range names {
		files := dirs[d]
		sort.Strings(files)
		shown := files
		if len(shown) > 12 {
			shown = shown[:12]
		}
		line := fmt.Sprintf("%s/ (%d files): %s", d, len(files), strings.Join(shown, ", "))
		if len(files) > len(shown) {
			line += ", ..."
		}
		if b.Len()+len(line)+1 > maxChars {
			b.WriteString("... (tree truncated)")
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
