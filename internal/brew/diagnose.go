package brew

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Extraction is what actually happened at the counter.
type Extraction struct {
	DoseGrams  float64
	WaterGrams float64
	Seconds    int
	TempC      float64

	GrindLabel   string
	GrindMicrons int

	// TDS is a refractometer reading as a percentage. Zero means it was not
	// measured, which is the usual case.
	TDS float64

	Notes string
}

// Ratio is the water-to-coffee ratio this extraction actually used, which is
// not necessarily the one the recipe asks for.
func (e Extraction) Ratio() float64 {
	if e.DoseGrams <= 0 {
		return 0
	}
	return e.WaterGrams / e.DoseGrams
}

// Severity is how far a variable strayed.
type Severity string

const (
	OnTarget Severity = "on target"
	Minor    Severity = "minor"
	Major    Severity = "major"
)

// Finding is one variable compared against the recipe.
type Finding struct {
	Variable  string
	Expected  string
	Actual    string
	Deviation string
	Severity  Severity
}

// Recommendation is a single change to make next.
type Recommendation struct {
	Change string
	Reason string
}

// Diagnosis is the comparison of an extraction against its recipe.
type Diagnosis struct {
	Findings []Finding

	// Primary is the one change to make. Coffee is diagnosed by moving one
	// variable at a time: changing two makes the next cup uninterpretable,
	// because whichever way it moves, nothing says which change did it.
	Primary *Recommendation

	// Watch lists things that are off but should be left alone this round, so
	// the barista knows they were seen and not forgotten.
	Watch []string

	// ExtractionYield is the percentage of the ground coffee that dissolved,
	// computed only when a refractometer reading was supplied.
	ExtractionYield float64
}

// OnTarget reports whether nothing needs changing.
func (d Diagnosis) OnTarget() bool { return d.Primary == nil }

// absorptionPerGram is how much brew water a gram of coffee retains in the bed.
// It is what separates the water poured from the coffee in the cup, and without
// it an extraction yield comes out flattering and wrong.
const absorptionPerGram = 2.0

// Deviation bands, as a fraction of the target.
const (
	minorBand = 0.10
	majorBand = 0.25
)

// Diagnose compares an extraction against the recipe it was meant to follow.
//
// now is passed in rather than read from the clock so that a diagnosis is a
// pure function of its inputs: the same extraction judged on the same day gives
// the same answer in a test as it does at the counter.
func Diagnose(recipe Recipe, method Method, actual Extraction, coffee Coffee, now time.Time) Diagnosis {
	var d Diagnosis

	d.appendTiming(recipe, actual)
	d.appendRatio(recipe, actual)
	d.appendTemperature(recipe, actual)

	if actual.TDS > 0 && actual.DoseGrams > 0 {
		d.ExtractionYield = extractionYield(actual)
	}

	d.decide(recipe, method, actual, coffee, now)
	return d
}

func (d *Diagnosis) appendTiming(recipe Recipe, actual Extraction) {
	if recipe.TargetMinSeconds == 0 && recipe.TargetMaxSeconds == 0 {
		return
	}
	if actual.Seconds <= 0 {
		return
	}

	target := float64(recipe.TargetMinSeconds+recipe.TargetMaxSeconds) / 2
	finding := Finding{
		Variable: "extraction time",
		Expected: recipe.TargetWindow(),
		Actual:   clock(actual.Seconds),
	}

	// Falling outside the window is never on target, however small the miss
	// looks against the clock. The window is the tolerance the shop chose, so
	// a proportional band may grade how far out a brew was, never whether it
	// was out at all.
	switch {
	case actual.Seconds < recipe.TargetMinSeconds:
		short := recipe.TargetMinSeconds - actual.Seconds
		finding.Deviation = fmt.Sprintf("%ds fast", short)
		finding.Severity = atLeast(band(float64(short)/target), Minor)
	case actual.Seconds > recipe.TargetMaxSeconds:
		over := actual.Seconds - recipe.TargetMaxSeconds
		finding.Deviation = fmt.Sprintf("%ds slow", over)
		finding.Severity = atLeast(band(float64(over)/target), Minor)
	default:
		finding.Deviation = "within window"
		finding.Severity = OnTarget
	}
	d.Findings = append(d.Findings, finding)
}

func (d *Diagnosis) appendRatio(recipe Recipe, actual Extraction) {
	got := actual.Ratio()
	if got <= 0 || recipe.Ratio <= 0 {
		return
	}

	drift := math.Abs(got-recipe.Ratio) / recipe.Ratio
	finding := Finding{
		Variable: "ratio",
		Expected: fmt.Sprintf("1:%.1f", recipe.Ratio),
		Actual:   fmt.Sprintf("1:%.1f", got),
		Severity: band(drift),
	}

	switch {
	case finding.Severity == OnTarget:
		finding.Deviation = "as written"
	case got > recipe.Ratio:
		finding.Deviation = "weaker than the recipe"
	default:
		finding.Deviation = "stronger than the recipe"
	}
	d.Findings = append(d.Findings, finding)
}

func (d *Diagnosis) appendTemperature(recipe Recipe, actual Extraction) {
	if recipe.WaterTempC <= 0 || actual.TempC <= 0 {
		return
	}

	delta := actual.TempC - recipe.WaterTempC
	finding := Finding{
		Variable: "water temperature",
		Expected: fmt.Sprintf("%.0f °C", recipe.WaterTempC),
		Actual:   fmt.Sprintf("%.0f °C", actual.TempC),
	}

	// Temperature is judged in degrees, not proportionally: two degrees is two
	// degrees whether the recipe asks for 92 or 96.
	switch magnitude := math.Abs(delta); {
	case magnitude < 1:
		finding.Deviation, finding.Severity = "as written", OnTarget
	case magnitude <= 3:
		finding.Deviation, finding.Severity = fmt.Sprintf("%+.0f °C", delta), Minor
	default:
		finding.Deviation, finding.Severity = fmt.Sprintf("%+.0f °C", delta), Major
	}
	d.Findings = append(d.Findings, finding)
}

// decide picks the single change to make next.
//
// The order is the order a barista would work in: fix the thing that is most
// wrong, and prefer the variable that moves the result most for the method in
// hand. Everything else is recorded to watch rather than changed, because two
// changes at once make the next cup say nothing.
func (d *Diagnosis) decide(recipe Recipe, method Method, actual Extraction, coffee Coffee, now time.Time) {
	timing := d.finding("extraction time")
	ratio := d.finding("ratio")
	temperature := d.finding("water temperature")

	// A ratio that is badly off is not a brewing fault, it is a different
	// drink. Nothing else can be judged until the recipe is actually followed.
	if ratio != nil && ratio.Severity == Major {
		d.Primary = &Recommendation{
			Change: fmt.Sprintf("brew at the recipe's 1:%.1f before changing anything else", recipe.Ratio),
			Reason: fmt.Sprintf("the cup was made at 1:%.1f, which is %s; timing and taste cannot be read against a recipe that was not followed",
				actual.Ratio(), ratio.Deviation),
		}
		d.watchOthers(timing, temperature)
		return
	}

	if timing != nil && timing.Severity != OnTarget {
		d.Primary = grindAdjustment(recipe, method, actual, timing)
		d.watchOthers(ratio, temperature)
		return
	}

	if temperature != nil && temperature.Severity != OnTarget {
		direction := "raise"
		effect := "sour, thin cups mean the extraction never got going"
		if actual.TempC > recipe.WaterTempC {
			direction, effect = "lower", "harsh, drying cups mean it went too far"
		}
		d.Primary = &Recommendation{
			Change: fmt.Sprintf("%s the water to %.0f °C", direction, recipe.WaterTempC),
			Reason: fmt.Sprintf("the brew ran %s off the recipe; %s", temperature.Deviation, effect),
		}
		d.watchOthers(ratio)
		return
	}

	// Everything measured is on target. If the barista still disliked the cup,
	// the variable at fault is one nobody wrote down.
	if rest := coffee.Rest(now); rest == TooFresh || rest == Stale {
		d.Watch = append(d.Watch, fmt.Sprintf(
			"every measured variable is on target, but the coffee is %s; the bean, not the brew, is the thing that changed", rest))
	}
}

// grindAdjustment turns a timing miss into a grind change sized to the miss.
func grindAdjustment(recipe Recipe, method Method, actual Extraction, timing *Finding) *Recommendation {
	fast := actual.Seconds < recipe.TargetMinSeconds

	direction := "finer"
	because := "water ran through the bed too quickly to dissolve what it should have"
	if !fast {
		direction = "coarser"
		because = "water sat in the bed longer than it should have, pulling out what comes last and tastes worst"
	}

	// An immersion brew has no flow to slow down: the water sits for as long as
	// the timer says either way, so grind changes how fast it extracts, not how
	// long it takes.
	if method.Kind == Immersion {
		return &Recommendation{
			Change: fmt.Sprintf("grind %s, and hold the steep time at %s", direction, recipe.TargetWindow()),
			Reason: fmt.Sprintf("the brew was %s; in an immersion the grind sets how fast extraction runs, not how long the water is in contact, so the timer stays where the recipe puts it",
				timing.Deviation),
		}
	}

	step := "one step"
	if timing.Severity == Major {
		step = "two steps"
	}
	if method.Kind == Pressure {
		step = "a small step"
		if timing.Severity == Major {
			step = "a noticeable step"
		}
	}

	return &Recommendation{
		Change: fmt.Sprintf("grind %s %s and leave dose, water and temperature exactly where they are", step, direction),
		Reason: fmt.Sprintf("the brew was %s: %s. Grind is the strongest lever on a %s, so move it alone and the next cup will say whether it was enough",
			timing.Deviation, because, method.Kind),
	}
}

func (d *Diagnosis) watchOthers(findings ...*Finding) {
	for _, finding := range findings {
		if finding == nil || finding.Severity == OnTarget {
			continue
		}
		d.Watch = append(d.Watch, fmt.Sprintf(
			"%s is also off (%s, %s) — leave it for the next round so this change can be read on its own",
			finding.Variable, finding.Actual, finding.Deviation))
	}
}

func (d *Diagnosis) finding(variable string) *Finding {
	for i := range d.Findings {
		if d.Findings[i].Variable == variable {
			return &d.Findings[i]
		}
	}
	return nil
}

// extractionYield is the share of the ground coffee that ended up dissolved in
// the cup.
//
// It uses the beverage mass, not the water poured: the bed keeps roughly two
// grams per gram of coffee, and counting that water as if it reached the cup
// inflates the figure into the range everyone wants to see.
func extractionYield(actual Extraction) float64 {
	beverage := actual.WaterGrams - actual.DoseGrams*absorptionPerGram
	if beverage <= 0 {
		return 0
	}
	return round1(actual.TDS * beverage / actual.DoseGrams)
}

// atLeast raises a severity to a floor, for a variable whose own limits already
// say it is out of bounds.
func atLeast(got, floor Severity) Severity {
	if rank(got) >= rank(floor) {
		return got
	}
	return floor
}

func band(deviation float64) Severity {
	switch {
	case deviation < minorBand:
		return OnTarget
	case deviation < majorBand:
		return Minor
	default:
		return Major
	}
}

// Report renders a diagnosis for a person to read.
func (d Diagnosis) Report() string {
	var b strings.Builder

	fmt.Fprintln(&b, "measured against the recipe:")
	for _, finding := range d.Findings {
		fmt.Fprintf(&b, "  %-18s expected %-14s actual %-10s %s (%s)\n",
			finding.Variable, finding.Expected, finding.Actual,
			finding.Deviation, finding.Severity)
	}

	if d.ExtractionYield > 0 {
		fmt.Fprintf(&b, "  %-18s %.1f%%\n", "extraction yield", d.ExtractionYield)
	}

	fmt.Fprintln(&b)
	if d.Primary == nil {
		fmt.Fprintln(&b, "change nothing: every measured variable is within its window.")
	} else {
		fmt.Fprintf(&b, "change one thing: %s\n", d.Primary.Change)
		fmt.Fprintf(&b, "  why: %s\n", d.Primary.Reason)
	}

	if len(d.Watch) > 0 {
		fmt.Fprintln(&b, "\nnoted, but leave alone this round:")
		for _, item := range d.Watch {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	return b.String()
}
