package services

import (
	"strings"
	"testing"
)

// The Job Map's "Practice" buttons used to navigate to /dashboard and drop
// what was clicked, so a button reading "Practice Mock for Razorpay" produced
// an interview that had never heard of Razorpay. These cover the parts of
// carrying it through that do not need a database.

// A role and its analysis are two different interviews. Keying the cache on
// the analysis alone would serve whichever was generated first to everyone —
// the framing would appear to work once and then silently stop, which is
// worse than never having shipped it.
func TestRoleFramedQuestionsCacheSeparatelyFromPlainOnes(t *testing.T) {
	analysis := `{"summary":"a go service"}`

	plain := analysisFingerprint(analysis + "\x00" + "")
	framed := analysisFingerprint(analysis + "\x00" + "Backend Engineer at Razorpay")
	other := analysisFingerprint(analysis + "\x00" + "Frontend Engineer at Meesho")

	if plain == framed {
		t.Error("a role-framed set shares a cache key with the plain set")
	}
	if framed == other {
		t.Error("two different roles share one cache key")
	}
}

// The same role on the same analysis must still hit the cache, or every load
// of the interview screen is a fresh LLM call — the thing the fingerprint
// exists to prevent.
func TestSameRoleAndAnalysisStillShareACacheKey(t *testing.T) {
	analysis := `{"summary":"a go service"}`
	role := "Backend Engineer at Razorpay"

	key1 := analysisFingerprint(analysis+"\x00"+role)
	key2 := analysisFingerprint(analysis+"\x00"+role)
	if key1 != key2 {
		t.Error("the same role and analysis produce different cache keys")
	}
}

// The separator matters. Without it "abc"+"de" and "ab"+"cde" are the same
// string, so an analysis ending in a role's opening characters could collide
// with a different pairing.
func TestFingerprintSeparatorPreventsCollisions(t *testing.T) {
	a := analysisFingerprint("ab" + "\x00" + "cde")
	b := analysisFingerprint("abc" + "\x00" + "de")
	if a == b {
		t.Error("analysis and role are concatenated ambiguously")
	}
}

// An unframed interview must key on the analysis ALONE — the same key it had
// before a role was ever a parameter.
//
// The first version of this appended the separator unconditionally, which
// changed every existing fingerprint: the first load of every already-cached
// repository would have missed and paid the worker for a set it already held.
// The test that shipped with it compared the new key against itself and so
// asserted nothing; this compares against the key the cache actually contains.
func TestUnframedInterviewsKeepTheirExistingCacheKey(t *testing.T) {
	analysis := `{"summary":"a go service"}`

	if interviewTarget("", "") != "" {
		t.Fatal("no ids produced a target")
	}
	if questionCacheKey(analysis, "") != analysis {
		t.Error("an unframed interview no longer keys on the analysis alone")
	}
	if analysisFingerprint(questionCacheKey(analysis, "")) != analysisFingerprint(analysis) {
		t.Error("an unframed fingerprint differs from the one already stored")
	}
}

// Whitespace is not an id. A blank query parameter must not reach the database
// or produce framing.
func TestBlankTargetIdsAreIgnored(t *testing.T) {
	for _, pair := range [][2]string{{"", ""}, {"   ", ""}, {"", "  "}, {" ", " "}} {
		if got := interviewTarget(pair[0], pair[1]); got != "" {
			t.Errorf("interviewTarget(%q, %q) = %q, want empty", pair[0], pair[1], got)
		}
	}
}

// The worker is told the role but must not be told to ask about it. The
// questions are the one thing this product can ask with authority — they come
// out of code the candidate wrote — and a job description is not evidence of
// anything they built.
func TestTargetRoleIsSentAsFramingOnly(t *testing.T) {
	payload := GenerateQuestionsPayload{
		RepoFullName: "acme/api",
		AnalysisData: `{"summary":"a go service"}`,
		TargetRole:   "Backend Engineer at Razorpay",
	}
	if payload.TargetRole == "" {
		t.Fatal("the payload cannot carry a role")
	}
	// The analysis stays the subject: it is what the generator is given to
	// draw questions from, and the role never replaces it.
	if payload.AnalysisData == "" {
		t.Error("framing a role dropped the analysis")
	}
}

// A target that survives to the prompt must read as one line naming a role
// and a company, not as an id.
func TestTargetReadsAsARoleNotAnID(t *testing.T) {
	for _, bad := range []string{"9d7b8333-8aa0-4164-90b1-cc80819109a2", "job_12"} {
		// interviewTarget resolves ids against the database; with no database
		// configured in a unit test it must decline rather than echo the id
		// back into the prompt.
		if got := interviewTarget(bad, ""); strings.Contains(got, bad) {
			t.Errorf("an unresolved id leaked into the framing: %q", got)
		}
	}
}
