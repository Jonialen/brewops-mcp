package brew

import (
	"strings"
	"testing"
)

func baselineRoast() RoastProfile {
	return RoastProfile{
		Batch:           "L-2401",
		ChargeTempC:     198,
		ChargeGrams:     12000,
		TurningPointSec: 60,
		DryEndSec:       270, // 4:30
		FirstCrackSec:   540, // 9:00
		FirstCrackTempC: 196.5,
		DropSec:         660, // 11:00
		DropTempC:       205.0,
	}
}

// Development time ratio is the number roasters compare batches on, because it
// survives a change in total roast time.
func TestDevelopmentTimeRatio(t *testing.T) {
	p := baselineRoast()

	if got := p.DevelopmentSeconds(); got != 120 {
		t.Errorf("DevelopmentSeconds() = %d, want 120", got)
	}
	// 120 / 660 = 18.2 %
	if got := p.DevelopmentTimeRatio(); got < 18.1 || got > 18.3 {
		t.Errorf("DevelopmentTimeRatio() = %.1f, want about 18.2", got)
	}
	if got := p.MaillardSeconds(); got != 270 {
		t.Errorf("MaillardSeconds() = %d, want 270", got)
	}

	// A profile that never reached the drop cannot report a ratio.
	if got := (RoastProfile{FirstCrackSec: 540}).DevelopmentTimeRatio(); got != 0 {
		t.Errorf("an incomplete profile reported a ratio of %.1f", got)
	}
}

// The shop's fourth case: two batches of the same coffee that cup differently.
func TestCompareProfilesFindsTheLandmarkThatMoved(t *testing.T) {
	earlier := baselineRoast()

	// First crack came a minute late and the roast ran longer with it.
	later := baselineRoast()
	later.Batch = "L-2408"
	later.FirstCrackSec = 600
	later.DropSec = 750
	later.DropTempC = 207.5

	c := CompareProfiles(earlier, later)

	if !c.Significant() {
		t.Fatal("two batches a minute apart were called identical")
	}
	if c.Summary == "" {
		t.Fatal("no summary was produced")
	}

	moved := map[string]ProfileDifference{}
	for _, diff := range c.Differences {
		moved[diff.Landmark] = diff
	}

	if got := moved["first crack"]; got.Severity == OnTarget {
		t.Errorf("first crack moved 60s and was graded %s", got.Severity)
	}
	if got := moved["total roast"]; got.Change != "+90s" {
		t.Errorf("total roast change = %q, want +90s", got.Change)
	}

	// Drying ended at the same moment but first crack came a minute later, so
	// the browning phase between them stretched by exactly that minute. That is
	// the finding a roaster wants: the extra heat went into maillard, not into
	// drying.
	if got := moved["maillard"]; got.Change != "+60s" {
		t.Errorf("maillard change = %q, want +60s", got.Change)
	}
	if got := moved["maillard"]; got.Severity == OnTarget {
		t.Errorf("a minute of extra browning was graded %s", got.Severity)
	}

	// Drying itself did not move, and must not be reported as though it had.
	if got := moved["dry end"]; got.Severity != OnTarget {
		t.Errorf("dry end was graded %s (%s) though it did not move", got.Severity, got.Change)
	}

	report := c.Report(earlier, later)
	for _, want := range []string{"L-2401", "L-2408", "first crack"} {
		if !strings.Contains(report, want) {
			t.Errorf("report does not mention %q:\n%s", want, report)
		}
	}
}

// A roaster does not chase five seconds in a twelve-minute roast. Calling that
// significant would bury the changes that are.
func TestSmallDifferencesAreNotSignificant(t *testing.T) {
	earlier := baselineRoast()

	later := baselineRoast()
	later.Batch = "L-2402"
	later.FirstCrackSec += 8
	later.DropSec += 10
	later.DropTempC += 1.0

	c := CompareProfiles(earlier, later)

	if c.Significant() {
		var noisy []string
		for _, diff := range c.Differences {
			if diff.Severity != OnTarget {
				noisy = append(noisy, diff.Landmark+" "+diff.Change)
			}
		}
		t.Errorf("noise was graded significant: %v", noisy)
	}
	if !strings.Contains(c.Summary, "not in these profiles") {
		t.Errorf("summary = %q, want it to say the profiles do not explain a difference", c.Summary)
	}
}

// Development moving on its own is the change a roaster most wants surfaced.
func TestDevelopmentChangeIsSurfaced(t *testing.T) {
	earlier := baselineRoast()

	later := baselineRoast()
	later.Batch = "L-2409"
	later.DropSec = 780 // 60s of extra development, first crack unchanged

	c := CompareProfiles(earlier, later)

	var development ProfileDifference
	for _, diff := range c.Differences {
		if diff.Landmark == "development" {
			development = diff
		}
	}
	if development.Severity != Major {
		t.Errorf("120s of extra development was graded %s", development.Severity)
	}
	if development.Effect == "" {
		t.Error("no effect was described for the development change")
	}

	var ratio ProfileDifference
	for _, diff := range c.Differences {
		if diff.Landmark == "development time ratio" {
			ratio = diff
		}
	}
	if ratio.Severity == OnTarget {
		t.Errorf("the development ratio moved from 18.2%% and was graded %s", ratio.Severity)
	}
}

// A landmark nobody recorded is left out rather than compared against zero.
func TestMissingLandmarksAreSkipped(t *testing.T) {
	earlier := RoastProfile{Batch: "A", FirstCrackSec: 540, DropSec: 660}
	later := RoastProfile{Batch: "B", FirstCrackSec: 560, DropSec: 690}

	c := CompareProfiles(earlier, later)

	for _, diff := range c.Differences {
		switch diff.Landmark {
		case "turning point", "dry end", "drop temperature", "first crack temperature":
			t.Errorf("%s was compared though neither batch recorded it", diff.Landmark)
		}
	}
	if len(c.Differences) == 0 {
		t.Error("nothing was compared even though both batches share landmarks")
	}
}
