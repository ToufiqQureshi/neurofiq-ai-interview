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

// "manager" sat in the Senior bucket, so every Account Manager and Product
// Manager was filed as senior: 983 roles carried the word with no seniority
// marker, and the facet strip read Senior 2,608 against Mid 19. Managing is a
// kind of work, not a rung.
func TestClassifyLevelManagerIsNotSeniority(t *testing.T) {
	unspecified := []string{
		"Account Manager",
		"Product Manager",
		"Customer Success Manager",
		"Engineering Manager",
	}
	for _, title := range unspecified {
		if got := ClassifyLevel(title); got != "Unspecified" {
			t.Errorf("ClassifyLevel(%q) = %q, want Unspecified", title, got)
		}
	}

	// The words that do carry a rung still do, including on a manager title.
	graded := map[string]string{
		"Senior Product Manager":      "Senior",
		"Staff Software Engineer":     "Senior",
		"Head of Engineering":         "Lead",
		"Software Engineer III":       "Senior",
		"Software Engineer II":        "Mid",
		"Junior Data Analyst":         "Junior",
		"Software Engineering Intern": "Fresher",
	}
	for title, want := range graded {
		if got := ClassifyLevel(title); got != want {
			t.Errorf("ClassifyLevel(%q) = %q, want %q", title, got, want)
		}
	}
}

// The roman numerals are matched with a leading space so they find "Engineer
// II" without firing on any title that merely contains those letters.
func TestClassifyLevelRomanNumeralsNeedTheirOwnWord(t *testing.T) {
	for _, title := range []string{"Hawaii Regional Sales", "Skiing Instructor"} {
		if got := ClassifyLevel(title); got == "Mid" {
			t.Errorf("ClassifyLevel(%q) = Mid, want anything else", title)
		}
	}
}
