package services

import "testing"

func TestClassifyField(t *testing.T) {
	cases := []struct{ title, department, want string }{
		// Bucket order matters: "Data Engineer" has to land in Data & AI, not
		// Engineering, which is why Data is checked first.
		{"Senior Data Engineer", "", "Data & AI"},
		{"Machine Learning Engineer", "", "Data & AI"},
		{"Backend Engineer", "Engineering", "Engineering"},
		{"Product Designer", "Design", "Design"},
	}
	for _, tc := range cases {
		if got := ClassifyField(tc.title, tc.department); got != tc.want {
			t.Errorf("ClassifyField(%q, %q) = %q, want %q", tc.title, tc.department, got, tc.want)
		}
	}
}

func TestClassifyLevelAdmitsWhenUnknown(t *testing.T) {
	// "Unspecified" is a real answer, not a fallback bug: most titles say
	// nothing about seniority, and guessing would be wrong more often than
	// admitting we don't know.
	if got := ClassifyLevel("Backend Engineer"); got != "Unspecified" {
		t.Errorf("expected Unspecified for a title with no seniority, got %q", got)
	}
	if got := ClassifyLevel("Senior Backend Engineer"); got == "Unspecified" {
		t.Errorf("expected a real level for an explicitly senior title, got %q", got)
	}
}
