// Package brew holds the domain of coffee preparation: what a shop knows about
// its beans, the recipes it has settled on, and the arithmetic and reasoning
// that turn one into the other.
//
// Everything in this package computes. That is the point of the server: a
// language model asked to scale a recipe will produce numbers that look right,
// and a shop that needs the same cup twice cannot use numbers that look right.
package brew

import (
	"fmt"
	"strings"
	"time"
)

// Process is how the fruit was removed from the seed. It is the single largest
// influence on how a coffee tastes before roasting enters the picture.
type Process string

const (
	Washed    Process = "washed"
	Natural   Process = "natural"
	Honey     Process = "honey"
	Anaerobic Process = "anaerobic"
)

// RoastLevel is how far the roast was taken.
type RoastLevel string

const (
	Light       RoastLevel = "light"
	MediumLight RoastLevel = "medium-light"
	Medium      RoastLevel = "medium"
	MediumDark  RoastLevel = "medium-dark"
	Dark        RoastLevel = "dark"
)

// Coffee is one lot in the shop's catalogue.
type Coffee struct {
	ID       int64
	Name     string
	Origin   string
	Region   string
	Variety  string
	Process  Process
	Roast    RoastLevel
	RoastOn  time.Time
	Altitude int // metres above sea level

	// Notes are the sensory descriptors the shop uses for this lot. They are
	// what a customer's request is matched against.
	Notes []string
}

// DaysOffRoast reports how long the coffee has been resting, relative to now.
//
// Age matters as much as any brewing variable: a coffee three days off roast is
// still degassing and will behave differently from the same lot at two weeks,
// which is why a diagnosis that ignores it can blame the grinder for the calendar.
func (c Coffee) DaysOffRoast(now time.Time) int {
	if c.RoastOn.IsZero() {
		return -1
	}
	return int(now.Sub(c.RoastOn).Hours() / 24)
}

// RestState describes where a coffee sits in its useful life.
type RestState string

const (
	TooFresh RestState = "too fresh"
	Resting  RestState = "resting"
	Peak     RestState = "peak"
	Fading   RestState = "fading"
	Stale    RestState = "stale"
	Unknown  RestState = "unknown"
)

// Rest windows in days off roast. Darker roasts degas faster and fade sooner,
// so the same calendar day means something different for each.
func (c Coffee) Rest(now time.Time) RestState {
	days := c.DaysOffRoast(now)
	if days < 0 {
		return Unknown
	}

	peakStart, peakEnd := 7, 21
	switch c.Roast {
	case Light:
		peakStart, peakEnd = 10, 28
	case MediumDark, Dark:
		peakStart, peakEnd = 4, 14
	}

	switch {
	case days < peakStart/2:
		return TooFresh
	case days < peakStart:
		return Resting
	case days <= peakEnd:
		return Peak
	case days <= peakEnd*2:
		return Fading
	default:
		return Stale
	}
}

// MatchesNotes scores how well this coffee answers a request for given flavours.
//
// The score is the share of requested notes that the lot claims, so asking for
// two notes and matching one scores 0.5 whether the lot lists three descriptors
// or ten. A lot is not rewarded for being described verbosely.
func (c Coffee) MatchesNotes(wanted []string) float64 {
	if len(wanted) == 0 {
		return 0
	}

	var matched int
	for _, want := range wanted {
		if c.hasNote(want) {
			matched++
		}
	}
	return float64(matched) / float64(len(wanted))
}

// hasNote reports whether this lot answers one requested descriptor.
//
// The request is broken into terms, each term is expanded into the specific
// descriptors a shop would actually write for it, and the lot's own notes are
// matched against all of them. That is what lets a request for "floral" find a
// coffee the roaster described as "jasmine, bergamot".
func (c Coffee) hasNote(want string) bool {
	for _, term := range termsOf(want) {
		for _, candidate := range expand(term) {
			if c.mentions(candidate) {
				return true
			}
		}
	}
	return false
}

// mentions reports whether any of the lot's notes contains a descriptor.
//
// The test runs one way only: the shop's note may be more specific than the
// descriptor, never less. "cocoa nib" answers "cocoa", and "stone fruit"
// answers "fruit".
//
// Matching the other way as well looks harmless and is not. Compound
// descriptors share head words across unrelated families: "orange blossom" is
// floral and "orange" is citrus, and a reverse match makes a bag of oranges
// answer a request for flowers.
func (c Coffee) mentions(candidate string) bool {
	if candidate == "" {
		return false
	}
	for _, note := range c.Notes {
		note = strings.ToLower(strings.TrimSpace(note))
		if note != "" && strings.Contains(note, candidate) {
			return true
		}
	}
	return false
}

// String renders a coffee for a person to read.
func (c Coffee) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s", c.Name, c.Origin)
	if c.Region != "" {
		fmt.Fprintf(&b, ", %s", c.Region)
	}
	fmt.Fprintf(&b, ") — %s, %s roast", c.Process, c.Roast)
	if len(c.Notes) > 0 {
		fmt.Fprintf(&b, "; notes: %s", strings.Join(c.Notes, ", "))
	}
	return b.String()
}
