package brew

import (
	"strings"
	"testing"
	"time"
)

var brewDay = time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)

func restedCoffee() Coffee {
	return Coffee{
		Name:    "Ethiopia Guji",
		Roast:   Light,
		RoastOn: brewDay.AddDate(0, 0, -14),
	}
}

func pourOver() Method { return Method{Name: "V60", Kind: PourOver} }

// The shop's own second case: a V60 that finished at 2:10 against a 2:45–3:10
// window. The answer is to grind finer, and to change nothing else.
func TestFastExtractionRecommendsAFinerGrind(t *testing.T) {
	actual := Extraction{DoseGrams: 21, WaterGrams: 350, Seconds: 130, TempC: 94}

	d := Diagnose(v60Recipe(), pourOver(), actual, restedCoffee(), brewDay)

	if d.OnTarget() {
		t.Fatal("a brew 35 seconds fast was called on target")
	}
	if !strings.Contains(d.Primary.Change, "finer") {
		t.Errorf("recommendation = %q, want a finer grind", d.Primary.Change)
	}

	// The whole point of the rule: everything else is held still.
	for _, held := range []string{"dose", "water", "temperature"} {
		if !strings.Contains(d.Primary.Change, held) {
			t.Errorf("recommendation does not say to hold %s: %q", held, d.Primary.Change)
		}
	}

	timing := findingFor(t, d, "extraction time")
	if timing.Severity == OnTarget {
		t.Errorf("timing was graded %s", timing.Severity)
	}
	if !strings.Contains(timing.Deviation, "fast") {
		t.Errorf("deviation = %q", timing.Deviation)
	}
}

func TestSlowExtractionRecommendsACoarserGrind(t *testing.T) {
	actual := Extraction{DoseGrams: 21, WaterGrams: 350, Seconds: 240, TempC: 94}

	d := Diagnose(v60Recipe(), pourOver(), actual, restedCoffee(), brewDay)

	if d.OnTarget() {
		t.Fatal("a brew 50 seconds slow was called on target")
	}
	if !strings.Contains(d.Primary.Change, "coarser") {
		t.Errorf("recommendation = %q, want a coarser grind", d.Primary.Change)
	}
}

// A brew inside its window with everything else in place needs no change, and
// saying so is as useful as any correction.
func TestOnTargetExtractionChangesNothing(t *testing.T) {
	actual := Extraction{DoseGrams: 21, WaterGrams: 350, Seconds: 178, TempC: 94}

	d := Diagnose(v60Recipe(), pourOver(), actual, restedCoffee(), brewDay)

	if !d.OnTarget() {
		t.Fatalf("an on-target brew was given a change: %+v", d.Primary)
	}
	if !strings.Contains(d.Report(), "change nothing") {
		t.Errorf("report does not say to leave it alone:\n%s", d.Report())
	}
}

// Two changes at once make the next cup uninterpretable. A second problem is
// recorded, not acted on.
func TestOnlyOneChangeIsRecommendedAtATime(t *testing.T) {
	// Fast and eight degrees cold: both are real, only one may move.
	actual := Extraction{DoseGrams: 21, WaterGrams: 350, Seconds: 130, TempC: 86}

	d := Diagnose(v60Recipe(), pourOver(), actual, restedCoffee(), brewDay)

	if d.Primary == nil {
		t.Fatal("no change was recommended")
	}
	if strings.Contains(d.Primary.Change, "temperature to") {
		t.Errorf("two variables were changed at once: %q", d.Primary.Change)
	}

	// The temperature must still be reported, or the barista thinks it was
	// missed.
	var mentioned bool
	for _, item := range d.Watch {
		if strings.Contains(item, "temperature") {
			mentioned = true
		}
	}
	if !mentioned {
		t.Errorf("the temperature problem was dropped instead of noted: %v", d.Watch)
	}
}

// A ratio badly off is not a brewing fault, it is a different drink. Nothing
// else can be read until the recipe is actually followed.
func TestBadlyWrongRatioIsFixedBeforeAnythingElse(t *testing.T) {
	// 21 g with 500 g of water is 1:23.8 against a recipe asking 1:16.7.
	actual := Extraction{DoseGrams: 21, WaterGrams: 500, Seconds: 130, TempC: 94}

	d := Diagnose(v60Recipe(), pourOver(), actual, restedCoffee(), brewDay)

	if d.Primary == nil {
		t.Fatal("no change was recommended")
	}
	if strings.Contains(d.Primary.Change, "grind") {
		t.Errorf("the grinder was blamed for a ratio that was never followed: %q", d.Primary.Change)
	}
	if !strings.Contains(d.Primary.Change, "16.7") {
		t.Errorf("recommendation = %q, want it to name the recipe's ratio", d.Primary.Change)
	}
}

// An immersion brew has no flow to slow down: the grind changes how fast it
// extracts, not how long the water is in contact.
func TestImmersionKeepsItsSteepTime(t *testing.T) {
	recipe := Recipe{
		Ratio: 15, WaterTempC: 95,
		TargetMinSeconds: 240, TargetMaxSeconds: 270,
	}
	french := Method{Name: "French Press", Kind: Immersion}
	actual := Extraction{DoseGrams: 30, WaterGrams: 450, Seconds: 180, TempC: 95}

	d := Diagnose(recipe, french, actual, restedCoffee(), brewDay)

	if d.Primary == nil {
		t.Fatal("no change was recommended")
	}
	if !strings.Contains(d.Primary.Change, "finer") {
		t.Errorf("recommendation = %q", d.Primary.Change)
	}
	if !strings.Contains(d.Primary.Change, "hold the steep time") {
		t.Errorf("an immersion was not told to hold its time: %q", d.Primary.Change)
	}
}

// A bigger miss earns a bigger correction.
func TestCorrectionSizeFollowsTheMiss(t *testing.T) {
	recipe := v60Recipe()
	coffee := restedCoffee()

	slight := Diagnose(recipe, pourOver(),
		Extraction{DoseGrams: 21, WaterGrams: 350, Seconds: 150, TempC: 94}, coffee, brewDay)
	severe := Diagnose(recipe, pourOver(),
		Extraction{DoseGrams: 21, WaterGrams: 350, Seconds: 70, TempC: 94}, coffee, brewDay)

	if !strings.Contains(slight.Primary.Change, "one step") {
		t.Errorf("a small miss got %q", slight.Primary.Change)
	}
	if !strings.Contains(severe.Primary.Change, "two steps") {
		t.Errorf("a large miss got %q", severe.Primary.Change)
	}
}

// The bed keeps water. Counting it as if it reached the cup inflates the yield
// into the range everyone wants to see.
func TestExtractionYieldUsesBeverageMassNotWaterPoured(t *testing.T) {
	actual := Extraction{DoseGrams: 21, WaterGrams: 350, Seconds: 178, TempC: 94, TDS: 1.35}

	d := Diagnose(v60Recipe(), pourOver(), actual, restedCoffee(), brewDay)

	// beverage = 350 - 21*2 = 308 g; yield = 1.35 * 308 / 21 = 19.8 %
	if d.ExtractionYield < 19.5 || d.ExtractionYield > 20.1 {
		t.Errorf("ExtractionYield = %.1f%%, want about 19.8%%", d.ExtractionYield)
	}

	// The naive figure, using all the water, would be 22.5 %.
	naive := actual.TDS * actual.WaterGrams / actual.DoseGrams
	if d.ExtractionYield >= naive {
		t.Errorf("yield %.1f%% did not account for water held in the bed (naive %.1f%%)",
			d.ExtractionYield, naive)
	}
}

func TestNoRefractometerReadingMeansNoYield(t *testing.T) {
	actual := Extraction{DoseGrams: 21, WaterGrams: 350, Seconds: 178, TempC: 94}

	d := Diagnose(v60Recipe(), pourOver(), actual, restedCoffee(), brewDay)
	if d.ExtractionYield != 0 {
		t.Errorf("a yield of %.1f%% was reported without a measurement", d.ExtractionYield)
	}
}

// When every measured variable is on target and the cup is still wrong, the
// variable at fault is one nobody wrote down.
func TestFreshCoffeeIsRaisedWhenNothingElseIsWrong(t *testing.T) {
	fresh := Coffee{Name: "Ethiopia Guji", Roast: Light, RoastOn: brewDay.AddDate(0, 0, -2)}
	actual := Extraction{DoseGrams: 21, WaterGrams: 350, Seconds: 178, TempC: 94}

	d := Diagnose(v60Recipe(), pourOver(), actual, fresh, brewDay)

	if !d.OnTarget() {
		t.Fatalf("a brew inside every window was given a change: %+v", d.Primary)
	}
	if len(d.Watch) == 0 || !strings.Contains(strings.Join(d.Watch, " "), "too fresh") {
		t.Errorf("the coffee's age was not raised: %v", d.Watch)
	}
}

func findingFor(t *testing.T, d Diagnosis, variable string) Finding {
	t.Helper()
	for _, finding := range d.Findings {
		if finding.Variable == variable {
			return finding
		}
	}
	t.Fatalf("no finding for %q in %+v", variable, d.Findings)
	return Finding{}
}
