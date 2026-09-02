package services

import "testing"

// The order of sectorKeywords is the design: what a company does beats how it
// is built. Doceree describes itself as an "AI-powered healthcare marketing
// platform" and belongs under Healthtech — filing it under AI would put it
// beside model shops and out of reach of anyone browsing health.
func TestClassifySectorPrefersTheDomainOverTheTooling(t *testing.T) {
	cases := map[string]string{
		"AI-powered healthcare marketing platform for physician engagement": "Healthtech",
		"New-age stockbroking and trading platform for Indian traders":      "Fintech",
		"Machine learning platform for building and serving models":         "AI",
		"B2B software for teams to manage workflow automation":              "SaaS",
		"Warehouse robotics and automation systems":                         "Deeptech",
		"Last mile delivery network for e-commerce":                         "Logistics",
		"Exam prep and test prep for students":                              "Edtech",
	}
	for description, want := range cases {
		if got := ClassifySector("", description); got != want {
			t.Errorf("ClassifySector(%q) = %q, want %q", description, got, want)
		}
	}
}

// An unmatched company gets no sector rather than "Other".
//
// "Other" is a claim: we looked, and it is genuinely none of these. A keyword
// miss is not that — it is "we could not tell". Leaving it empty also keeps a
// count of what the map cannot handle, which is the evidence that would
// justify reaching for a model later rather than guessing now.
func TestClassifySectorAdmitsWhenItCannotTell(t *testing.T) {
	for _, description := range []string{
		"We build things people love",
		"",
		"A company",
	} {
		if got := ClassifySector("", description); got != "" {
			t.Errorf("ClassifySector(%q) = %q, want \"\" (unknown)", description, got)
		}
	}
}

// A meta description is whatever the site chose to serve, including its cookie
// banner or a JS-required notice. Those would land on the card as the
// company's one-line pitch.
func TestCleanDescriptionRejectsNonDescriptions(t *testing.T) {
	for _, raw := range []string{
		"Please enable JavaScript to view this site",
		"This site uses cookies to improve your experience",
		"404 - page not found",
		"short",
	} {
		if got := cleanDescription(raw); got != "" {
			t.Errorf("cleanDescription(%q) = %q, want empty", raw, got)
		}
	}

	// A real one survives, with entities decoded and whitespace collapsed.
	got := cleanDescription("Employee health &amp; wellness   benefits platform for companies")
	want := "Employee health & wellness benefits platform for companies"
	if got != want {
		t.Errorf("cleanDescription() = %q, want %q", got, want)
	}
}

// Substring matching is only safe while the substrings are words. Each of
// these fired the wrong bucket before its keyword grew a leading space or a
// second word.
func TestClassifySectorAvoidsSubstringCollisions(t *testing.T) {
	cases := map[string]string{
		// "erp" lives inside "enterprise".
		"Enterprise security and compliance for large organisations": "",
		// "dating" lives inside "updating".
		"Automatically updating your product catalogue": "",
		// "learning platform" lives inside "machine learning platform".
		"Machine learning platform for building and serving models": "AI",
		// "warehouse" is real here, but "robotics" is the more specific claim.
		"Warehouse robotics and automation systems": "Deeptech",
	}
	for description, want := range cases {
		if got := ClassifySector("", description); got != want {
			t.Errorf("ClassifySector(%q) = %q, want %q", description, got, want)
		}
	}
}

// Only slug-derived names get replaced by what a homepage calls itself. A name
// that already reads like a company came from a page title the slug agreed
// with, and is better than a site banner.
func TestLooksLikeSlugFallback(t *testing.T) {
	replaceable := []string{"gokwik", "vmlenterprisesolutions", "oliverseapac", "brillio 2", "clirnet", ""}
	for _, name := range replaceable {
		if !looksLikeSlugFallback(name) {
			t.Errorf("looksLikeSlugFallback(%q) = false, want true", name)
		}
	}

	keep := []string{"Match Group", "Level AI", "WPP Media", "KRAFTON INDIA", "Z1 Tech", "Sarvam"}
	for _, name := range keep {
		if looksLikeSlugFallback(name) {
			t.Errorf("looksLikeSlugFallback(%q) = true, want false", name)
		}
	}
}

// A homepage name is only taken when the slug corroborates it — the same
// referee board discovery uses. Without that, a site whose og:site_name names
// something else would put one company's name over another's roles.
func TestSiteNameNeedsTheSlugToAgree(t *testing.T) {
	agree := map[string]string{
		"VML Enterprise Solutions": "vmlenterprisesolutions",
		"GoKwik":                   "gokwik",
		"Nutrabay":                 "nutrabay",
		"CLIRNET":                  "clirnet",
	}
	for siteName, slug := range agree {
		if !nameAgreesWithSlug(siteName, slug) {
			t.Errorf("nameAgreesWithSlug(%q, %q) = false, want true", siteName, slug)
		}
	}

	disagree := map[string]string{
		"CreditVidya":             "cred",
		"Sign in to your account": "gokwik",
		"Shopify":                 "nutrabay",
	}
	for siteName, slug := range disagree {
		if nameAgreesWithSlug(siteName, slug) {
			t.Errorf("nameAgreesWithSlug(%q, %q) = true, want false", siteName, slug)
		}
	}
}

// The two biggest boards in the directory had a description and no sector,
// because India's two largest white-collar employers by headcount were not in
// the vocabulary at all. These are their real meta descriptions.
func TestClassifySectorCoversServicesAndMedia(t *testing.T) {
	cases := map[string]string{
		"Brillio is a global leader in Enterprise Digital Transformation Solutions, providing strategic consulting services using emerging technologies.": "IT Services",
		"In a world reshaped by AI, media will be everywhere. We are WPP's global media collective, delivering intelligent growth for the AI era.":        "Media & Advertising",
		"Managed services and staff augmentation for enterprise IT":                                                                                       "IT Services",
		"Creative agency building brand campaigns for consumer brands":                                                                                    "Media & Advertising",
	}
	for description, want := range cases {
		if got := ClassifySector("", description); got != want {
			t.Errorf("ClassifySector(%q) = %q, want %q", description, got, want)
		}
	}

	// A consultancy that mentions machine learning is still a consultancy, and
	// a product company that does not is still a product company.
	if got := ClassifySector("", "Technology consulting for machine learning adoption"); got != "IT Services" {
		t.Errorf("consulting + ML = %q, want IT Services", got)
	}
	if got := ClassifySector("", "Machine learning platform for building and serving models"); got != "AI" {
		t.Errorf("ML platform = %q, want AI", got)
	}
}
