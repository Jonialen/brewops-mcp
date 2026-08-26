package brew

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Method is a way of making coffee. The kind decides which variables matter:
// an immersion brew has no flow to slow down, so grinding finer does something
// different there than it does in a pour-over.
type Method struct {
	ID   int64
	Name string
	Kind MethodKind
}

// MethodKind groups methods by how water meets coffee.
type MethodKind string

const (
	PourOver  MethodKind = "pour-over"
	Immersion MethodKind = "immersion"
	Pressure  MethodKind = "pressure"
)

// Recipe is the preparation a shop has settled on for one coffee on one method.
//
// Ratio is grams of water per gram of coffee, so 16.7 means 1:16.7. Storing the
// ratio rather than a fixed dose is what makes a recipe scalable at all: the
// numbers on the card are one instance of it, not the recipe itself.
type Recipe struct {
	ID       int64
	CoffeeID int64
	MethodID int64

	Ratio      float64
	WaterTempC float64

	GrindMicrons int
	GrindLabel   string

	// BloomRatio is grams of bloom water per gram of coffee. Zero means the
	// method has no bloom, which is the case for immersion and pressure.
	BloomRatio   float64
	BloomSeconds int

	TargetMinSeconds int
	TargetMaxSeconds int

	Notes string
}

// TargetWindow renders the expected extraction time.
func (r Recipe) TargetWindow() string {
	if r.TargetMinSeconds == 0 && r.TargetMaxSeconds == 0 {
		return "not recorded"
	}
	return fmt.Sprintf("%s–%s", clock(r.TargetMinSeconds), clock(r.TargetMaxSeconds))
}

// ScaledRecipe is a recipe worked out for a specific amount.
type ScaledRecipe struct {
	Recipe

	DoseGrams  float64
	WaterGrams float64
	BloomGrams float64

	// Pours breaks the water down into the stages a pour-over is built from.
	// It is empty for methods where all the water goes in at once.
	Pours []Pour
}

// Pour is one addition of water.
type Pour struct {
	Label      string
	Grams      float64
	AtSeconds  int
	TotalGrams float64
}

// ScaleToWater works a recipe out for a target amount of brewed water.
//
// This is the arithmetic a barista would otherwise do on the counter, and the
// reason it belongs on the server: the ratio is the shop's decision, and a dose
// derived from it is reproducible in a way an estimate is not.
func (r Recipe) ScaleToWater(waterGrams float64) (ScaledRecipe, error) {
	if waterGrams <= 0 {
		return ScaledRecipe{}, fmt.Errorf("water must be positive, got %.1fg", waterGrams)
	}
	if r.Ratio <= 0 {
		return ScaledRecipe{}, fmt.Errorf("recipe has no usable ratio (%.2f)", r.Ratio)
	}
	return r.scale(waterGrams/r.Ratio, waterGrams), nil
}

// ScaleToDose works a recipe out from a target amount of coffee, which is what
// a barista needs when the dose is fixed by a basket or by what is left in the
// bag.
func (r Recipe) ScaleToDose(doseGrams float64) (ScaledRecipe, error) {
	if doseGrams <= 0 {
		return ScaledRecipe{}, fmt.Errorf("dose must be positive, got %.1fg", doseGrams)
	}
	if r.Ratio <= 0 {
		return ScaledRecipe{}, fmt.Errorf("recipe has no usable ratio (%.2f)", r.Ratio)
	}
	return r.scale(doseGrams, doseGrams*r.Ratio), nil
}

func (r Recipe) scale(dose, water float64) ScaledRecipe {
	scaled := ScaledRecipe{
		Recipe: r,
		// A tenth of a gram is what a brewing scale reads. Carrying more
		// precision than the equipment can show invites a barista to chase a
		// number the scale will never display.
		DoseGrams:  round1(dose),
		WaterGrams: round1(water),
	}

	if r.BloomRatio > 0 {
		scaled.BloomGrams = round1(dose * r.BloomRatio)
		scaled.Pours = buildPours(scaled.BloomGrams, scaled.WaterGrams, r.BloomSeconds)
	}
	return scaled
}

// buildPours splits the water into a bloom and two even pours after it.
//
// Two pours rather than one because a single large addition floods the bed and
// drops the temperature further than the same water added in stages.
func buildPours(bloom, total float64, bloomSeconds int) []Pour {
	remaining := total - bloom
	if remaining <= 0 {
		return []Pour{{Label: "bloom", Grams: bloom, AtSeconds: 0, TotalGrams: bloom}}
	}

	half := round1(remaining / 2)
	pours := []Pour{
		{Label: "bloom", Grams: bloom, AtSeconds: 0, TotalGrams: bloom},
		{Label: "first pour", Grams: half, AtSeconds: bloomSeconds, TotalGrams: round1(bloom + half)},
	}

	// The last pour takes whatever rounding left over, so the totals add up to
	// the number on the scale rather than to something close to it.
	last := round1(total - bloom - half)
	pours = append(pours, Pour{
		Label:      "second pour",
		Grams:      last,
		AtSeconds:  bloomSeconds + 30,
		TotalGrams: total,
	})
	return pours
}

// Card renders a scaled recipe the way it would be written on a brew card.
func (s ScaledRecipe) Card(coffee Coffee, method Method, now time.Time) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s on %s\n", coffee.Name, method.Name)
	fmt.Fprintf(&b, "  dose        %.1f g\n", s.DoseGrams)
	fmt.Fprintf(&b, "  water       %.0f g\n", s.WaterGrams)
	fmt.Fprintf(&b, "  ratio       1:%.1f\n", s.Ratio)
	fmt.Fprintf(&b, "  temperature %.0f °C\n", s.WaterTempC)

	if s.GrindLabel != "" {
		fmt.Fprintf(&b, "  grind       %s", s.GrindLabel)
		if s.GrindMicrons > 0 {
			fmt.Fprintf(&b, " (~%d µm)", s.GrindMicrons)
		}
		fmt.Fprintln(&b)
	}

	if len(s.Pours) > 0 {
		fmt.Fprintf(&b, "  pours\n")
		for _, pour := range s.Pours {
			fmt.Fprintf(&b, "    %-12s %5.1f g at %s (total %.0f g)\n",
				pour.Label, pour.Grams, clock(pour.AtSeconds), pour.TotalGrams)
		}
	}

	fmt.Fprintf(&b, "  target time %s\n", s.TargetWindow())

	if rest := coffee.Rest(now); rest != Unknown {
		fmt.Fprintf(&b, "  rest        %d days off roast (%s)\n", coffee.DaysOffRoast(now), rest)
	}
	if s.Notes != "" {
		fmt.Fprintf(&b, "  notes       %s\n", s.Notes)
	}
	return b.String()
}

// clock renders seconds as minutes and seconds, which is how a barista reads a
// timer.
func clock(seconds int) string {
	if seconds <= 0 {
		return "0:00"
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
