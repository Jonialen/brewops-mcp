package brew

import "testing"

func guji() Coffee {
	return Coffee{
		Name:  "Ethiopia Guji Uraga",
		Notes: []string{"jasmine", "bergamot", "stone fruit", "black tea"},
	}
}

func brazil() Coffee {
	return Coffee{
		Name:  "Brazil Cerrado",
		Notes: []string{"peanut", "dark chocolate", "molasses", "low acidity"},
	}
}

// The request and the tasting note are written by different people. Somebody
// asks for "floral"; the roaster wrote "jasmine". Matching those as plain text
// finds nothing.
func TestBroadRequestsFindSpecificNotes(t *testing.T) {
	cases := []struct {
		request string
		coffee  Coffee
		want    bool
	}{
		{"floral", guji(), true}, // jasmine, bergamot
		{"fruity", guji(), true}, // stone fruit
		{"citrus", guji(), true}, // bergamot
		{"tea", guji(), true},    // black tea
		{"chocolate", guji(), false},
		{"nutty", guji(), false},

		{"chocolate", brazil(), true}, // dark chocolate
		{"nutty", brazil(), true},     // peanut
		{"sweet", brazil(), true},     // molasses
		{"floral", brazil(), false},
	}

	for _, tc := range cases {
		t.Run(tc.request+"/"+tc.coffee.Name, func(t *testing.T) {
			got := tc.coffee.MatchesNotes([]string{tc.request}) > 0
			if got != tc.want {
				t.Errorf("%q against %v = %v, want %v",
					tc.request, tc.coffee.Notes, got, tc.want)
			}
		})
	}
}

// A request arrives as somebody said it, filler and all.
func TestPhrasedRequestsAreUnderstood(t *testing.T) {
	coffee := Coffee{Notes: []string{"blackcurrant", "grapefruit", "brown sugar"}}

	for _, request := range []string{
		"bright acidity",
		"quite bright",
		"notes of grapefruit",
		"juicy",
		"acidic",
	} {
		if coffee.MatchesNotes([]string{request}) == 0 {
			t.Errorf("%q did not match %v", request, coffee.Notes)
		}
	}
}

// A multi-word descriptor means something its separate words do not, so the
// whole phrase is tried before it is broken up.
func TestMultiWordNotesMatchWhole(t *testing.T) {
	coffee := Coffee{Notes: []string{"stone fruit", "black tea"}}

	for _, request := range []string{"stone fruit", "black tea"} {
		if coffee.MatchesNotes([]string{request}) == 0 {
			t.Errorf("%q did not match its own note", request)
		}
	}
}

// The scoring still measures the share of the request that was answered, so a
// broadened match must not inflate it.
func TestCategoryMatchingKeepsTheScoreHonest(t *testing.T) {
	coffee := guji()

	// Two of three: floral and fruity match, chocolate does not.
	got := coffee.MatchesNotes([]string{"floral", "fruity", "chocolate"})
	if got < 0.66 || got > 0.67 {
		t.Errorf("score = %.2f, want two of three", got)
	}

	if all := coffee.MatchesNotes([]string{"floral", "fruity"}); all != 1.0 {
		t.Errorf("score for a fully answered request = %.2f", all)
	}
	if none := coffee.MatchesNotes([]string{"chocolate", "nutty"}); none != 0 {
		t.Errorf("score for an unanswered request = %.2f", none)
	}
}

// The shop's third case: a customer asking for a floral, pronounced acidity
// coffee for V60 has to reach the Guji, not miss it on a technicality.
func TestTheShopsRecommendationCaseIsAnswered(t *testing.T) {
	request := []string{"floral", "pronounced acidity"}

	if score := guji().MatchesNotes(request); score < 0.5 {
		t.Errorf("the Guji scored %.2f for a floral, acidic request", score)
	}
	if score := brazil().MatchesNotes(request); score > 0.5 {
		t.Errorf("a peanut and chocolate coffee scored %.2f for a floral request", score)
	}
}

// Compound descriptors share head words across unrelated families. "orange
// blossom" is floral and "orange" is citrus, and conflating them makes a bag of
// oranges answer a request for flowers.
func TestCompoundDescriptorsDoNotLeakAcrossFamilies(t *testing.T) {
	citrusy := Coffee{Notes: []string{"red apple", "caramel", "orange", "almond"}}

	if citrusy.MatchesNotes([]string{"floral"}) > 0 {
		t.Errorf("%v answered a request for floral", citrusy.Notes)
	}
	// It is genuinely citrus and genuinely bright, and must still say so.
	if citrusy.MatchesNotes([]string{"citrus"}) == 0 {
		t.Errorf("%v did not answer a request for citrus", citrusy.Notes)
	}
	if citrusy.MatchesNotes([]string{"bright acidity"}) == 0 {
		t.Errorf("%v did not answer a request for bright acidity", citrusy.Notes)
	}

	// The reverse case still has to work: the shop's note may be the more
	// specific of the two.
	nibs := Coffee{Notes: []string{"cocoa nib", "syrupy"}}
	if nibs.MatchesNotes([]string{"chocolate"}) == 0 {
		t.Errorf("%v did not answer a request for chocolate", nibs.Notes)
	}
}
