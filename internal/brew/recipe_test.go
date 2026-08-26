package brew

import (
	"math"
	"testing"
	"time"
)

func v60Recipe() Recipe {
	return Recipe{
		Ratio:            16.7,
		WaterTempC:       94,
		GrindMicrons:     650,
		GrindLabel:       "medium-fine",
		BloomRatio:       2.0,
		BloomSeconds:     45,
		TargetMinSeconds: 165, // 2:45
		TargetMaxSeconds: 190, // 3:10
	}
}

func closeTo(t *testing.T, label string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Errorf("%s = %.2f, want %.2f (±%.2f)", label, got, want, tolerance)
	}
}

// The worked example from the shop's own brief: 350 g of water on a 1:16.7
// recipe should call for about 21 g of coffee.
func TestScaleToWaterMatchesTheShopsExample(t *testing.T) {
	scaled, err := v60Recipe().ScaleToWater(350)
	if err != nil {
		t.Fatalf("ScaleToWater: %v", err)
	}

	closeTo(t, "dose", scaled.DoseGrams, 21.0, 0.1)
	closeTo(t, "water", scaled.WaterGrams, 350, 0.1)
	closeTo(t, "bloom", scaled.BloomGrams, 41.9, 0.2)
}

// A dose is what a barista has when the bag is nearly empty, and the water has
// to follow from it.
func TestScaleToDoseIsTheInverse(t *testing.T) {
	recipe := v60Recipe()

	fromWater, err := recipe.ScaleToWater(350)
	if err != nil {
		t.Fatalf("ScaleToWater: %v", err)
	}
	fromDose, err := recipe.ScaleToDose(fromWater.DoseGrams)
	if err != nil {
		t.Fatalf("ScaleToDose: %v", err)
	}

	closeTo(t, "round-tripped water", fromDose.WaterGrams, 350, 1.0)
}

// The pours have to add up to the number the scale will show. A recipe whose
// stages sum to something else is one a barista cannot follow.
func TestPoursSumToTheTotalWater(t *testing.T) {
	for _, water := range []float64{250, 333, 350, 500, 1000, 17} {
		scaled, err := v60Recipe().ScaleToWater(water)
		if err != nil {
			t.Fatalf("ScaleToWater(%v): %v", water, err)
		}

		var sum float64
		for _, pour := range scaled.Pours {
			sum += pour.Grams
		}
		closeTo(t, "sum of pours", sum, scaled.WaterGrams, 0.05)

		last := scaled.Pours[len(scaled.Pours)-1]
		closeTo(t, "running total of the last pour", last.TotalGrams, scaled.WaterGrams, 0.05)
	}
}

// A brewing scale reads to a tenth of a gram. Carrying more precision invites a
// barista to chase a number the scale will never show.
func TestScaledAmountsAreReadableOnAScale(t *testing.T) {
	scaled, err := v60Recipe().ScaleToWater(333)
	if err != nil {
		t.Fatalf("ScaleToWater: %v", err)
	}

	for label, value := range map[string]float64{
		"dose":  scaled.DoseGrams,
		"bloom": scaled.BloomGrams,
	} {
		if math.Abs(value*10-math.Round(value*10)) > 1e-9 {
			t.Errorf("%s = %v, which a scale cannot display", label, value)
		}
	}
}

// A method with no bloom gets no pour schedule invented for it.
func TestImmersionRecipeHasNoPours(t *testing.T) {
	recipe := Recipe{Ratio: 15, WaterTempC: 95, TargetMinSeconds: 240, TargetMaxSeconds: 270}

	scaled, err := recipe.ScaleToWater(500)
	if err != nil {
		t.Fatalf("ScaleToWater: %v", err)
	}
	if len(scaled.Pours) != 0 {
		t.Errorf("an immersion recipe was given a pour schedule: %+v", scaled.Pours)
	}
	if scaled.BloomGrams != 0 {
		t.Errorf("BloomGrams = %v for a recipe with no bloom", scaled.BloomGrams)
	}
}

func TestScalingRejectsImpossibleAmounts(t *testing.T) {
	recipe := v60Recipe()

	for _, water := range []float64{0, -1} {
		if _, err := recipe.ScaleToWater(water); err == nil {
			t.Errorf("ScaleToWater(%v) was accepted", water)
		}
	}
	for _, dose := range []float64{0, -5} {
		if _, err := recipe.ScaleToDose(dose); err == nil {
			t.Errorf("ScaleToDose(%v) was accepted", dose)
		}
	}

	broken := Recipe{Ratio: 0}
	if _, err := broken.ScaleToWater(350); err == nil {
		t.Error("a recipe with no ratio was scaled anyway")
	}
}

func TestRestStateFollowsRoastLevel(t *testing.T) {
	now := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		roast RoastLevel
		days  int
		want  RestState
	}{
		{Light, 2, TooFresh},
		{Light, 7, Resting},
		{Light, 14, Peak},
		{Light, 40, Fading},
		{Light, 100, Stale},
		// A dark roast degasses faster, so the same day means something else.
		{Dark, 6, Peak},
		{Medium, 14, Peak},
	}

	for _, tc := range cases {
		coffee := Coffee{Roast: tc.roast, RoastOn: now.AddDate(0, 0, -tc.days)}
		if got := coffee.Rest(now); got != tc.want {
			t.Errorf("%s roast at %d days = %s, want %s", tc.roast, tc.days, got, tc.want)
		}
	}

	if got := (Coffee{Roast: Light}).Rest(now); got != Unknown {
		t.Errorf("a coffee with no roast date reported %s, want %s", got, Unknown)
	}
}

// A lot is not rewarded for being described verbosely: the score is the share
// of what was asked for, not of what the lot lists.
func TestMatchesNotesScoresTheRequest(t *testing.T) {
	sparse := Coffee{Notes: []string{"jasmine", "bergamot"}}
	verbose := Coffee{Notes: []string{"jasmine", "bergamot", "peach", "honey", "black tea", "lime"}}

	wanted := []string{"jasmine", "bergamot"}
	if sparse.MatchesNotes(wanted) != 1.0 {
		t.Errorf("sparse lot scored %.2f for a full match", sparse.MatchesNotes(wanted))
	}
	if verbose.MatchesNotes(wanted) != 1.0 {
		t.Errorf("verbose lot scored %.2f for the same full match", verbose.MatchesNotes(wanted))
	}

	half := Coffee{Notes: []string{"jasmine", "cocoa"}}
	if got := half.MatchesNotes(wanted); got != 0.5 {
		t.Errorf("half match scored %.2f, want 0.50", got)
	}

	// A shop writes "stone fruit"; a customer asks for "fruit".
	stone := Coffee{Notes: []string{"stone fruit", "cocoa"}}
	if stone.MatchesNotes([]string{"fruit"}) == 0 {
		t.Error("a broader request did not match a more specific note")
	}

	if got := sparse.MatchesNotes(nil); got != 0 {
		t.Errorf("an empty request scored %.2f", got)
	}
}
