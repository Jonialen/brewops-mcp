package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Jonialen/brewops-mcp/internal/brew"
)

func testStore(t *testing.T) *Store {
	t.Helper()

	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenSeedsAWorkingCatalogue(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	coffees, err := s.Coffees(ctx)
	if err != nil {
		t.Fatalf("Coffees: %v", err)
	}
	if len(coffees) < 5 {
		t.Fatalf("catalogue holds %d coffees, want a shop's worth", len(coffees))
	}

	for _, coffee := range coffees {
		if coffee.Name == "" || coffee.Origin == "" {
			t.Errorf("incomplete record: %+v", coffee)
		}
		if len(coffee.Notes) == 0 {
			t.Errorf("%s has no sensory notes, so it can never be recommended", coffee.Name)
		}
		// A lot seeded with a fixed date would be stale by the time anybody
		// ran this; the dates are relative for exactly that reason.
		if rest := coffee.Rest(time.Now()); rest == Stale(t) {
			t.Errorf("%s seeded already stale (%d days off roast)",
				coffee.Name, coffee.DaysOffRoast(time.Now()))
		}
	}

	methods, err := s.Methods(ctx)
	if err != nil {
		t.Fatalf("Methods: %v", err)
	}
	if len(methods) < 4 {
		t.Errorf("only %d methods are set up", len(methods))
	}
}

// Stale is wrapped so the test reads as a comparison rather than a package
// reference in the middle of a condition.
func Stale(*testing.T) brew.RestState { return brew.Stale }

// The name reaching this server came from a person speaking to a model.
// Somebody asking for "the Guji" means the lot filed as "Ethiopia Guji Uraga".
func TestFindCoffeeIsForgivingAboutNames(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for _, name := range []string{
		"Ethiopia Guji Uraga",
		"ethiopia guji uraga",
		"Guji",
		"guji uraga",
	} {
		coffee, err := s.FindCoffee(ctx, name)
		if err != nil {
			t.Errorf("FindCoffee(%q): %v", name, err)
			continue
		}
		if coffee.Name != "Ethiopia Guji Uraga" {
			t.Errorf("FindCoffee(%q) = %q", name, coffee.Name)
		}
	}
}

func TestFindCoffeeReportsAmbiguityAndAbsence(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Two Ethiopian lots are seeded, so the origin alone is not enough.
	_, err := s.FindCoffee(ctx, "Ethiopia")
	if err == nil {
		t.Error("an ambiguous name was resolved silently")
	} else if !strings.Contains(err.Error(), "more than one") {
		t.Errorf("error = %q, want it to explain the ambiguity", err)
	}

	if _, err := s.FindCoffee(ctx, "Sumatra Mandheling"); err == nil {
		t.Error("a coffee not in the catalogue was found")
	}
	if _, err := s.FindCoffee(ctx, "  "); err == nil {
		t.Error("an empty name was accepted")
	}
}

// The brief's worked example has to be in the data, or the demonstration in the
// brief cannot be reproduced.
func TestTheBriefsExampleRecipeIsPresent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	coffee, err := s.FindCoffee(ctx, "Ethiopia Guji Uraga")
	if err != nil {
		t.Fatalf("FindCoffee: %v", err)
	}
	method, err := s.FindMethod(ctx, "V60")
	if err != nil {
		t.Fatalf("FindMethod: %v", err)
	}

	recipe, err := s.Recipe(ctx, coffee.ID, method.ID)
	if err != nil {
		t.Fatalf("Recipe: %v", err)
	}
	if recipe.Ratio != 16.7 {
		t.Errorf("ratio = %.1f, want 16.7", recipe.Ratio)
	}

	// 350 g of water on that ratio is the 21 g the brief quotes.
	scaled, err := recipe.ScaleToWater(350)
	if err != nil {
		t.Fatalf("ScaleToWater: %v", err)
	}
	if scaled.DoseGrams < 20.9 || scaled.DoseGrams > 21.1 {
		t.Errorf("dose for 350 g = %.1f g, want about 21 g", scaled.DoseGrams)
	}
}

func TestFindMethodToleratesSpacing(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for _, name := range []string{"French Press", "french press", "FrenchPress", "v60", "V60"} {
		if _, err := s.FindMethod(ctx, name); err != nil {
			t.Errorf("FindMethod(%q): %v", name, err)
		}
	}
	if _, err := s.FindMethod(ctx, "Siphon"); err == nil {
		t.Error("a method that is not set up was found")
	}
}

func TestMissingRecipeIsDistinguishable(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	coffee, _ := s.FindCoffee(ctx, "Brazil Cerrado")
	method, _ := s.FindMethod(ctx, "V60")

	_, err := s.Recipe(ctx, coffee.ID, method.ID)
	if err == nil {
		t.Fatal("a recipe that was never written was returned")
	}
	if !IsNoRecipe(err) {
		t.Errorf("error = %v, want it to be recognisable as a missing recipe", err)
	}

	// The caller can then say which methods the coffee does have.
	methods, err := s.MethodsFor(ctx, coffee.ID)
	if err != nil {
		t.Fatalf("MethodsFor: %v", err)
	}
	if len(methods) == 0 {
		t.Error("the coffee has no recipes at all")
	}
}

// The two Guji batches are the shop's fourth case and have to differ enough to
// be worth comparing.
func TestSeededRoastProfilesSupportTheComparisonCase(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	coffee, err := s.FindCoffee(ctx, "Ethiopia Guji Uraga")
	if err != nil {
		t.Fatalf("FindCoffee: %v", err)
	}

	profiles, err := s.RoastProfiles(ctx, coffee.ID)
	if err != nil {
		t.Fatalf("RoastProfiles: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("got %d profiles, want the two batches the case needs", len(profiles))
	}

	comparison := brew.CompareProfiles(profiles[0], profiles[1])
	if !comparison.Significant() {
		t.Error("the two seeded batches do not differ enough to explain a difference in the cup")
	}

	found, err := s.FindRoastProfile(ctx, coffee.ID, "L-2408")
	if err != nil {
		t.Fatalf("FindRoastProfile: %v", err)
	}
	if found.Batch != "L-2408" {
		t.Errorf("batch = %q", found.Batch)
	}

	_, err = s.FindRoastProfile(ctx, coffee.ID, "L-9999")
	if err == nil {
		t.Error("a batch that does not exist was found")
	} else if !strings.Contains(err.Error(), "L-2401") {
		t.Errorf("error = %q, want it to list the batches that do exist", err)
	}
}

func TestExtractionsRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	coffee, _ := s.FindCoffee(ctx, "Ethiopia Guji Uraga")
	method, _ := s.FindMethod(ctx, "V60")

	actual := brew.Extraction{
		DoseGrams: 21, WaterGrams: 350, Seconds: 130, TempC: 94,
		GrindLabel: "medium-fine", Notes: "ran fast, tasted thin",
	}
	if err := s.RecordExtraction(ctx, coffee.ID, method.ID, actual, 6, time.Now()); err != nil {
		t.Fatalf("RecordExtraction: %v", err)
	}

	recorded, err := s.RecentExtractions(ctx, coffee.ID, method.ID, 5)
	if err != nil {
		t.Fatalf("RecentExtractions: %v", err)
	}
	if len(recorded) != 1 {
		t.Fatalf("got %d records, want 1", len(recorded))
	}
	if recorded[0].Seconds != 130 || recorded[0].Rating != 6 {
		t.Errorf("record = %+v", recorded[0])
	}
	if recorded[0].BrewedAt.IsZero() {
		t.Error("the brew time was not stored")
	}
}

// Opening an existing database must not seed it a second time.
func TestReopeningDoesNotDuplicateTheCatalogue(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/brewops.db"

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	before, _ := first.Coffees(ctx)
	first.Close()

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()

	after, _ := second.Coffees(ctx)
	if len(after) != len(before) {
		t.Errorf("catalogue grew from %d to %d on reopening", len(before), len(after))
	}
}
