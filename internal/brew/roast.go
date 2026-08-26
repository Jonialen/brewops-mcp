package brew

import (
	"fmt"
	"math"
	"strings"
)

// RoastProfile is one batch as it came off the roaster.
//
// The times are seconds from charge, which is how a roaster reads them: the
// clock starts when the beans go in, and every landmark after that is measured
// against that moment rather than against the wall.
type RoastProfile struct {
	ID       int64
	CoffeeID int64
	Batch    string

	ChargeTempC float64
	ChargeGrams float64

	// TurningPoint is when the beans stop cooling the drum and start heating.
	TurningPointSec int

	// DryEnd is the end of the drying phase, where the bean stops losing free
	// moisture and browning begins.
	DryEndSec int

	FirstCrackSec   int
	FirstCrackTempC float64

	DropSec   int
	DropTempC float64

	Notes string
}

// DevelopmentSeconds is the time between first crack and the drop, which is
// where most of what a roaster controls about flavour actually happens.
func (p RoastProfile) DevelopmentSeconds() int {
	if p.DropSec <= 0 || p.FirstCrackSec <= 0 {
		return 0
	}
	return p.DropSec - p.FirstCrackSec
}

// DevelopmentTimeRatio is development as a share of the whole roast.
//
// Roasters compare batches on this rather than on raw development time, because
// a batch that ran two minutes longer overall is not comparable to a shorter one
// on seconds alone.
func (p RoastProfile) DevelopmentTimeRatio() float64 {
	if p.DropSec <= 0 {
		return 0
	}
	return round1(float64(p.DevelopmentSeconds()) / float64(p.DropSec) * 100)
}

// DryingSeconds is the time up to the end of the drying phase.
func (p RoastProfile) DryingSeconds() int { return p.DryEndSec }

// MaillardSeconds is the browning phase, between drying and first crack.
func (p RoastProfile) MaillardSeconds() int {
	if p.FirstCrackSec <= 0 || p.DryEndSec <= 0 {
		return 0
	}
	return p.FirstCrackSec - p.DryEndSec
}

// ProfileDifference is one landmark that moved between two batches.
type ProfileDifference struct {
	Landmark string
	Earlier  string
	Later    string
	Change   string
	Severity Severity

	// Effect is what a roaster would expect this change to do to the cup.
	Effect string
}

// ProfileComparison is the whole comparison of two batches.
type ProfileComparison struct {
	Differences []ProfileDifference

	// Summary names the single change most likely to explain a difference the
	// roaster noticed in the cup.
	Summary string
}

// Significant reports whether anything moved enough to explain a difference in
// the cup.
func (c ProfileComparison) Significant() bool {
	for _, diff := range c.Differences {
		if diff.Severity != OnTarget {
			return true
		}
	}
	return false
}

// Thresholds in seconds. A roaster does not chase a five-second difference in a
// twelve-minute roast, and calling one significant would bury the changes that
// are.
const (
	minorSeconds = 15
	majorSeconds = 45

	minorDegrees = 2.0
	majorDegrees = 5.0

	minorRatioPoints = 1.0
	majorRatioPoints = 2.5
)

// CompareProfiles reports what moved between two batches of the same coffee.
func CompareProfiles(earlier, later RoastProfile) ProfileComparison {
	var c ProfileComparison

	c.addSeconds("turning point", earlier.TurningPointSec, later.TurningPointSec,
		"a later turning point means the drum was colder or the charge heavier; the whole roast shifts with it")
	c.addSeconds("dry end", earlier.DryEndSec, later.DryEndSec,
		"a longer drying phase mutes acidity; a shorter one leaves it sharp and can leave the bean underdeveloped inside")
	c.addSeconds("maillard", earlier.MaillardSeconds(), later.MaillardSeconds(),
		"a longer browning phase builds body and caramel sweetness at the cost of the brighter, more delicate notes")
	c.addSeconds("first crack", earlier.FirstCrackSec, later.FirstCrackSec,
		"first crack arriving at a different time means the heat applied before it differed")
	c.addSeconds("development", earlier.DevelopmentSeconds(), later.DevelopmentSeconds(),
		"more development trades acidity for sweetness and body; too much flattens the cup entirely")
	c.addSeconds("total roast", earlier.DropSec, later.DropSec,
		"a longer roast overall, with everything else held, usually reads as heavier and less distinct")

	c.addDegrees("first crack temperature", earlier.FirstCrackTempC, later.FirstCrackTempC,
		"first crack at a different temperature points at the probe or at how fast heat was being applied")
	c.addDegrees("drop temperature", earlier.DropTempC, later.DropTempC,
		"drop temperature sets the roast level; a couple of degrees is a visible difference in the cup")

	c.addRatio(earlier.DevelopmentTimeRatio(), later.DevelopmentTimeRatio())

	c.summarise(earlier, later)
	return c
}

func (c *ProfileComparison) addSeconds(landmark string, earlier, later int, effect string) {
	if earlier <= 0 || later <= 0 {
		return
	}

	delta := later - earlier
	c.Differences = append(c.Differences, ProfileDifference{
		Landmark: landmark,
		Earlier:  clock(earlier),
		Later:    clock(later),
		Change:   signedSeconds(delta),
		Severity: secondBand(delta),
		Effect:   effect,
	})
}

func (c *ProfileComparison) addDegrees(landmark string, earlier, later float64, effect string) {
	if earlier <= 0 || later <= 0 {
		return
	}

	delta := later - earlier
	c.Differences = append(c.Differences, ProfileDifference{
		Landmark: landmark,
		Earlier:  fmt.Sprintf("%.1f °C", earlier),
		Later:    fmt.Sprintf("%.1f °C", later),
		Change:   fmt.Sprintf("%+.1f °C", delta),
		Severity: degreeBand(delta),
		Effect:   effect,
	})
}

func (c *ProfileComparison) addRatio(earlier, later float64) {
	if earlier <= 0 || later <= 0 {
		return
	}

	delta := later - earlier
	c.Differences = append(c.Differences, ProfileDifference{
		Landmark: "development time ratio",
		Earlier:  fmt.Sprintf("%.1f%%", earlier),
		Later:    fmt.Sprintf("%.1f%%", later),
		Change:   fmt.Sprintf("%+.1f points", delta),
		Severity: ratioBand(delta),
		Effect:   "the share of the roast spent after first crack; the number roasters compare batches on, because it survives a change in total time",
	})
}

// summarise names the largest movement, which is where a roaster looks first.
func (c *ProfileComparison) summarise(earlier, later RoastProfile) {
	var worst *ProfileDifference
	for i := range c.Differences {
		if c.Differences[i].Severity == OnTarget {
			continue
		}
		if worst == nil || rank(c.Differences[i].Severity) > rank(worst.Severity) {
			worst = &c.Differences[i]
		}
	}

	if worst == nil {
		c.Summary = fmt.Sprintf(
			"batches %s and %s are the same roast within the tolerance a roaster works to; if the cups differ, the cause is not in these profiles",
			earlier.Batch, later.Batch)
		return
	}

	c.Summary = fmt.Sprintf("%s moved most (%s, %s): %s",
		worst.Landmark, worst.Change, worst.Severity, worst.Effect)
}

func signedSeconds(delta int) string {
	if delta >= 0 {
		return fmt.Sprintf("+%ds", delta)
	}
	return fmt.Sprintf("%ds", delta)
}

func secondBand(delta int) Severity {
	switch magnitude := math.Abs(float64(delta)); {
	case magnitude < minorSeconds:
		return OnTarget
	case magnitude < majorSeconds:
		return Minor
	default:
		return Major
	}
}

func degreeBand(delta float64) Severity {
	switch magnitude := math.Abs(delta); {
	case magnitude < minorDegrees:
		return OnTarget
	case magnitude < majorDegrees:
		return Minor
	default:
		return Major
	}
}

func ratioBand(delta float64) Severity {
	switch magnitude := math.Abs(delta); {
	case magnitude < minorRatioPoints:
		return OnTarget
	case magnitude < majorRatioPoints:
		return Minor
	default:
		return Major
	}
}

func rank(s Severity) int {
	switch s {
	case Major:
		return 2
	case Minor:
		return 1
	default:
		return 0
	}
}

// Report renders a comparison for a person to read.
func (c ProfileComparison) Report(earlier, later RoastProfile) string {
	var b strings.Builder

	fmt.Fprintf(&b, "batch %s compared against batch %s\n\n", later.Batch, earlier.Batch)
	fmt.Fprintf(&b, "  %-26s %-10s %-10s %-12s %s\n", "landmark", earlier.Batch, later.Batch, "change", "significance")

	for _, diff := range c.Differences {
		fmt.Fprintf(&b, "  %-26s %-10s %-10s %-12s %s\n",
			diff.Landmark, diff.Earlier, diff.Later, diff.Change, diff.Severity)
	}

	fmt.Fprintf(&b, "\n%s\n", c.Summary)

	if c.Significant() {
		fmt.Fprintln(&b, "\nwhat moved, and what it does:")
		for _, diff := range c.Differences {
			if diff.Severity == OnTarget {
				continue
			}
			fmt.Fprintf(&b, "  - %s (%s): %s\n", diff.Landmark, diff.Change, diff.Effect)
		}
	}
	return b.String()
}
