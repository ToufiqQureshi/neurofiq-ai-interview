package services

import (
	"strings"
	"testing"
)

// The facet filter decides which companies a visitor can see, and three call
// sites share it. A filter that silently stops matching looks exactly like an
// empty directory, so the clause it builds is pinned here.

// Unknown is one option covering three states: never set, set to empty, and
// set to the literal string. 145 of 275 companies have no stage at all, so an
// Unknown that matched only the literal would leave the majority of the
// directory unreachable from every option shown — the bug it exists to fix.
func TestUnknownFacetReachesUnrecordedRows(t *testing.T) {
	sql, args := facetClause("stage", UnknownFacetValue, "")

	for _, want := range []string{"stage IS NULL", "stage = ''"} {
		if !strings.Contains(sql, want) {
			t.Errorf("Unknown filter cannot reach %s rows: %s", want, sql)
		}
	}
	if len(args) != 1 || args[0] != UnknownFacetValue {
		t.Errorf("Unknown filter does not also match the literal value: %v", args)
	}
}

// The Unknown clause is ORed internally and ANDed with every other filter. A
// bare OR chain binds looser than the surrounding AND, which would turn
// "hiring in Pune AND stage unknown" into "hiring in Pune, OR anything with a
// blank stage" — a filter that returns more rows the more you narrow it.
func TestUnknownFacetClauseIsParenthesised(t *testing.T) {
	sql, _ := facetClause("stage", UnknownFacetValue, "")
	if !strings.HasPrefix(sql, "(") || !strings.HasSuffix(sql, ")") {
		t.Errorf("OR clause is not parenthesised, so AND would bind tighter: %s", sql)
	}
}

// TotalOpenRoles and JobFacets both join companies to jobs, where an
// unqualified column name is ambiguous. Postgres rejects such a statement
// rather than guessing, so the filter would not narrow — it would 500.
func TestFacetFilterQualifiesColumnOnJoins(t *testing.T) {
	sql, _ := facetClause("sector", "Fintech", "companies")
	if !strings.Contains(sql, "companies.sector") {
		t.Errorf("joined query does not qualify the column: %s", sql)
	}

	plain, _ := facetClause("sector", "Fintech", "")
	if strings.Contains(plain, ".") {
		t.Errorf("unjoined query qualified the column anyway: %s", plain)
	}
}

// An ordinary value is exact-match and parameterised — never interpolated,
// since it arrives straight off a query string.
func TestOrdinaryFacetValueIsAPlaceholder(t *testing.T) {
	sql, args := facetClause("sector", "Fintech", "")
	if sql != "sector = ?" {
		t.Errorf("got %q, want %q", sql, "sector = ?")
	}
	if len(args) != 1 || args[0] != "Fintech" {
		t.Errorf("value not passed as an argument: %v", args)
	}
}

// Options are what the directory holds. Blank and "Unknown" are one option,
// not two, and the collapse must match what facetClause does when it is
// chosen — otherwise an option is offered that returns nothing.
func TestFacetOptionsCollapseBlankAndUnknownIntoOne(t *testing.T) {
	got := collapseFacetValues([]string{"Seed", "", "Series A", UnknownFacetValue, "  ", "Series C"})
	want := []string{"Seed", "Series A", "Series C", UnknownFacetValue}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// A column with nothing missing must not grow an Unknown option. An option
// that returns nothing is the same drift the hardcoded lists had, where
// Pre-seed, Gaming and Consumer were offered against zero companies.
func TestFacetOptionsOmitUnknownWhenNothingIsMissing(t *testing.T) {
	for _, v := range collapseFacetValues([]string{"AI", "Fintech"}) {
		if v == UnknownFacetValue {
			t.Error("Unknown offered for a column where every row has a value")
		}
	}
}

// Every option the directory offers must be one the filter can act on. This
// is the invariant the two hardcoded lists broke in both directions.
func TestEveryOfferedOptionIsSelectable(t *testing.T) {
	// The real stage values in the directory, blanks included.
	stored := []string{"Seed", "Bootstrapped", "Series A", "Series C+", "Series B",
		"Series C", UnknownFacetValue, "Public", "Acquired", "Series H", "", ""}

	for _, option := range collapseFacetValues(stored) {
		sql, args := facetClause("stage", option, "")
		if sql == "" || len(args) == 0 {
			t.Errorf("option %q produces no filter", option)
		}
	}
}
