package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Jonialen/brewops-mcp/internal/brew"
)

func (s *Set) listCoffees(ctx context.Context, _ json.RawMessage) (string, error) {
	coffees, err := s.store.Coffees(ctx)
	if err != nil {
		return "", err
	}
	if len(coffees) == 0 {
		return "", fmt.Errorf("the catalogue is empty")
	}

	now := s.now()
	var b strings.Builder
	fmt.Fprintf(&b, "%d coffees in the catalogue\n\n", len(coffees))

	for _, coffee := range coffees {
		methods, err := s.store.MethodsFor(ctx, coffee.ID)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(&b, "%s\n", coffee.Name)
		fmt.Fprintf(&b, "  origin   %s", coffee.Origin)
		if coffee.Region != "" {
			fmt.Fprintf(&b, " — %s", coffee.Region)
		}
		fmt.Fprintf(&b, "\n  process  %s, %s roast\n", coffee.Process, coffee.Roast)
		fmt.Fprintf(&b, "  notes    %s\n", strings.Join(coffee.Notes, ", "))

		if days := coffee.DaysOffRoast(now); days >= 0 {
			fmt.Fprintf(&b, "  rest     %d days off roast (%s)\n", days, coffee.Rest(now))
		}
		if len(methods) > 0 {
			fmt.Fprintf(&b, "  recipes  %s\n", joinMethods(methods))
		} else {
			fmt.Fprintf(&b, "  recipes  none recorded\n")
		}
		fmt.Fprintln(&b)
	}
	return b.String(), nil
}

func (s *Set) getCoffee(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Name string `json:"name"`
	}
	if err := decode(raw, &args); err != nil {
		return "", err
	}

	coffee, err := s.store.FindCoffee(ctx, args.Name)
	if err != nil {
		return "", err
	}
	methods, err := s.store.MethodsFor(ctx, coffee.ID)
	if err != nil {
		return "", err
	}

	now := s.now()
	var b strings.Builder

	fmt.Fprintf(&b, "%s\n", coffee.Name)
	fmt.Fprintf(&b, "  origin    %s", coffee.Origin)
	if coffee.Region != "" {
		fmt.Fprintf(&b, " — %s", coffee.Region)
	}
	fmt.Fprintln(&b)

	if coffee.Variety != "" {
		fmt.Fprintf(&b, "  variety   %s\n", coffee.Variety)
	}
	if coffee.Altitude > 0 {
		fmt.Fprintf(&b, "  altitude  %d masl\n", coffee.Altitude)
	}
	fmt.Fprintf(&b, "  process   %s\n", coffee.Process)
	fmt.Fprintf(&b, "  roast     %s\n", coffee.Roast)
	fmt.Fprintf(&b, "  notes     %s\n", strings.Join(coffee.Notes, ", "))

	if days := coffee.DaysOffRoast(now); days >= 0 {
		fmt.Fprintf(&b, "  rest      %d days off roast (%s)\n", days, coffee.Rest(now))
	}
	if len(methods) > 0 {
		fmt.Fprintf(&b, "  recipes   %s\n", joinMethods(methods))
	}

	profiles, err := s.store.RoastProfiles(ctx, coffee.ID)
	if err == nil && len(profiles) > 0 {
		labels := make([]string, 0, len(profiles))
		for _, p := range profiles {
			labels = append(labels, p.Batch)
		}
		fmt.Fprintf(&b, "  batches   %s\n", strings.Join(labels, ", "))
	}
	return b.String(), nil
}

func (s *Set) getRecipe(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Coffee string `json:"coffee"`
		Method string `json:"method"`
	}
	if err := decode(raw, &args); err != nil {
		return "", err
	}

	coffee, method, recipe, err := s.pairing(ctx, args.Coffee, args.Method)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s on %s, as recorded by the shop\n", coffee.Name, method.Name)
	fmt.Fprintf(&b, "  ratio        1:%.1f\n", recipe.Ratio)
	fmt.Fprintf(&b, "  temperature  %.0f °C\n", recipe.WaterTempC)

	if recipe.GrindLabel != "" {
		fmt.Fprintf(&b, "  grind        %s", recipe.GrindLabel)
		if recipe.GrindMicrons > 0 {
			fmt.Fprintf(&b, " (~%d µm)", recipe.GrindMicrons)
		}
		fmt.Fprintln(&b)
	}
	if recipe.BloomRatio > 0 {
		fmt.Fprintf(&b, "  bloom        %.1fx the dose for %ds\n", recipe.BloomRatio, recipe.BloomSeconds)
	}

	fmt.Fprintf(&b, "  target time  %s\n", recipe.TargetWindow())
	if recipe.Notes != "" {
		fmt.Fprintf(&b, "  notes        %s\n", recipe.Notes)
	}

	fmt.Fprintf(&b, "\nThis is a ratio, not a fixed dose: use scale_recipe to work it out for an amount.\n")
	return b.String(), nil
}

func (s *Set) scaleRecipe(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Coffee     string  `json:"coffee"`
		Method     string  `json:"method"`
		WaterGrams float64 `json:"water_grams"`
		DoseGrams  float64 `json:"dose_grams"`
	}
	if err := decode(raw, &args); err != nil {
		return "", err
	}

	coffee, method, recipe, err := s.pairing(ctx, args.Coffee, args.Method)
	if err != nil {
		return "", err
	}

	switch {
	case args.WaterGrams > 0 && args.DoseGrams > 0:
		return "", fmt.Errorf(
			"give either water_grams or dose_grams, not both: the ratio 1:%.1f fixes whichever one you leave out",
			recipe.Ratio)
	case args.WaterGrams <= 0 && args.DoseGrams <= 0:
		return "", fmt.Errorf("give either water_grams or dose_grams so the recipe can be scaled")
	}

	var scaled brew.ScaledRecipe
	if args.WaterGrams > 0 {
		scaled, err = recipe.ScaleToWater(args.WaterGrams)
	} else {
		scaled, err = recipe.ScaleToDose(args.DoseGrams)
	}
	if err != nil {
		return "", err
	}
	return scaled.Card(coffee, method, s.now()), nil
}

func (s *Set) diagnoseExtraction(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Coffee     string  `json:"coffee"`
		Method     string  `json:"method"`
		Seconds    int     `json:"seconds"`
		DoseGrams  float64 `json:"dose_grams"`
		WaterGrams float64 `json:"water_grams"`
		TempC      float64 `json:"temp_c"`
		GrindLabel string  `json:"grind_label"`
		TDS        float64 `json:"tds"`
		TasteNotes string  `json:"taste_notes"`
	}
	if err := decode(raw, &args); err != nil {
		return "", err
	}

	coffee, method, recipe, err := s.pairing(ctx, args.Coffee, args.Method)
	if err != nil {
		return "", err
	}
	if args.Seconds <= 0 {
		return "", fmt.Errorf("the extraction time is needed to diagnose the brew")
	}
	if args.DoseGrams <= 0 || args.WaterGrams <= 0 {
		return "", fmt.Errorf("the dose and the water are needed to work out the ratio actually used")
	}

	actual := brew.Extraction{
		DoseGrams:  args.DoseGrams,
		WaterGrams: args.WaterGrams,
		Seconds:    args.Seconds,
		TempC:      args.TempC,
		GrindLabel: args.GrindLabel,
		TDS:        args.TDS,
		Notes:      args.TasteNotes,
	}

	diagnosis := brew.Diagnose(recipe, method, actual, coffee, s.now())

	var b strings.Builder
	fmt.Fprintf(&b, "%s on %s\n\n", coffee.Name, method.Name)
	b.WriteString(diagnosis.Report())

	if args.TasteNotes != "" {
		fmt.Fprintf(&b, "\nthe barista tasted: %s\n", args.TasteNotes)
	}
	return b.String(), nil
}

func (s *Set) recommendCoffee(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Notes  []string `json:"notes"`
		Method string   `json:"method"`
		Limit  int      `json:"limit"`
	}
	if err := decode(raw, &args); err != nil {
		return "", err
	}
	if len(args.Notes) == 0 {
		return "", fmt.Errorf("give at least one flavour note to match against")
	}
	if args.Limit <= 0 {
		args.Limit = 3
	}

	coffees, err := s.store.Coffees(ctx)
	if err != nil {
		return "", err
	}

	// Restricting by method has to happen before scoring, or the best match
	// gets recommended and then turns out to be unbrewable on the machine the
	// barista is standing at.
	var restrictTo string
	if args.Method != "" {
		method, err := s.store.FindMethod(ctx, args.Method)
		if err != nil {
			return "", err
		}
		restrictTo = method.Name

		var brewable []brew.Coffee
		for _, coffee := range coffees {
			methods, err := s.store.MethodsFor(ctx, coffee.ID)
			if err != nil {
				return "", err
			}
			for _, m := range methods {
				if m.ID == method.ID {
					brewable = append(brewable, coffee)
					break
				}
			}
		}
		coffees = brewable
	}

	type scored struct {
		coffee brew.Coffee
		score  float64
	}

	var ranked []scored
	for _, coffee := range coffees {
		if score := coffee.MatchesNotes(args.Notes); score > 0 {
			ranked = append(ranked, scored{coffee, score})
		}
	}

	if len(ranked) == 0 {
		wanted := strings.Join(args.Notes, ", ")
		if restrictTo != "" {
			return "", fmt.Errorf("nothing in the catalogue with a %s recipe matches %s", restrictTo, wanted)
		}
		return "", fmt.Errorf("nothing in the catalogue matches %s", wanted)
	}

	now := s.now()
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		// Between equal matches, offer the one drinking best today.
		return restRank(ranked[i].coffee.Rest(now)) > restRank(ranked[j].coffee.Rest(now))
	})

	if len(ranked) > args.Limit {
		ranked = ranked[:args.Limit]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "matching %s", strings.Join(args.Notes, ", "))
	if restrictTo != "" {
		fmt.Fprintf(&b, " with a %s recipe", restrictTo)
	}
	fmt.Fprintf(&b, ", from what the shop has in stock:\n\n")

	for i, entry := range ranked {
		coffee := entry.coffee
		fmt.Fprintf(&b, "%d. %s (%.0f%% of what was asked for)\n", i+1, coffee.Name, entry.score*100)
		fmt.Fprintf(&b, "     %s, %s roast — %s\n", coffee.Process, coffee.Roast, strings.Join(coffee.Notes, ", "))

		if days := coffee.DaysOffRoast(now); days >= 0 {
			fmt.Fprintf(&b, "     %d days off roast (%s)\n", days, coffee.Rest(now))
		}

		methods, err := s.store.MethodsFor(ctx, coffee.ID)
		if err == nil && len(methods) > 0 {
			fmt.Fprintf(&b, "     recipes for %s\n", joinMethods(methods))
		}
		fmt.Fprintln(&b)
	}
	return b.String(), nil
}

func restRank(state brew.RestState) int {
	switch state {
	case brew.Peak:
		return 3
	case brew.Resting, brew.Fading:
		return 2
	case brew.TooFresh:
		return 1
	default:
		return 0
	}
}

func (s *Set) recordExtraction(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Coffee     string  `json:"coffee"`
		Method     string  `json:"method"`
		Seconds    int     `json:"seconds"`
		DoseGrams  float64 `json:"dose_grams"`
		WaterGrams float64 `json:"water_grams"`
		TempC      float64 `json:"temp_c"`
		GrindLabel string  `json:"grind_label"`
		Rating     int     `json:"rating"`
		Notes      string  `json:"notes"`
	}
	if err := decode(raw, &args); err != nil {
		return "", err
	}

	coffee, err := s.store.FindCoffee(ctx, args.Coffee)
	if err != nil {
		return "", err
	}
	method, err := s.store.FindMethod(ctx, args.Method)
	if err != nil {
		return "", err
	}
	if args.Seconds <= 0 || args.DoseGrams <= 0 || args.WaterGrams <= 0 {
		return "", fmt.Errorf("a recorded brew needs its time, dose and water")
	}
	if args.Rating < 0 || args.Rating > 10 {
		return "", fmt.Errorf("rating must be between 1 and 10, got %d", args.Rating)
	}

	actual := brew.Extraction{
		DoseGrams:  args.DoseGrams,
		WaterGrams: args.WaterGrams,
		Seconds:    args.Seconds,
		TempC:      args.TempC,
		GrindLabel: args.GrindLabel,
		Notes:      args.Notes,
	}

	now := s.now()
	if err := s.store.RecordExtraction(ctx, coffee.ID, method.ID, actual, args.Rating, now); err != nil {
		return "", err
	}

	recent, err := s.store.RecentExtractions(ctx, coffee.ID, method.ID, 5)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "recorded: %s on %s, %.1f g to %.0f g in %s\n",
		coffee.Name, method.Name, actual.DoseGrams, actual.WaterGrams, clock(actual.Seconds))
	fmt.Fprintf(&b, "\nthe last %d brews of this pairing:\n", len(recent))

	for _, past := range recent {
		fmt.Fprintf(&b, "  %s  %.1f g / %.0f g  %s  1:%.1f",
			past.BrewedAt.Format("2006-01-02 15:04"), past.DoseGrams, past.WaterGrams,
			clock(past.Seconds), past.Ratio())
		if past.Rating > 0 {
			fmt.Fprintf(&b, "  rated %d/10", past.Rating)
		}
		if past.Notes != "" {
			fmt.Fprintf(&b, "  — %s", past.Notes)
		}
		fmt.Fprintln(&b)
	}
	return b.String(), nil
}

func (s *Set) listRoastBatches(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Coffee string `json:"coffee"`
	}
	if err := decode(raw, &args); err != nil {
		return "", err
	}

	coffee, err := s.store.FindCoffee(ctx, args.Coffee)
	if err != nil {
		return "", err
	}
	profiles, err := s.store.RoastProfiles(ctx, coffee.ID)
	if err != nil {
		return "", err
	}
	if len(profiles) == 0 {
		return "", fmt.Errorf("no roast batches are recorded for %s", coffee.Name)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "roast batches of %s\n\n", coffee.Name)
	fmt.Fprintf(&b, "  %-10s %-12s %-10s %-12s %-8s %s\n",
		"batch", "first crack", "drop", "development", "ratio", "notes")

	for _, p := range profiles {
		fmt.Fprintf(&b, "  %-10s %-12s %-10s %-12s %-8s %s\n",
			p.Batch, clock(p.FirstCrackSec), clock(p.DropSec),
			clock(p.DevelopmentSeconds()),
			fmt.Sprintf("%.1f%%", p.DevelopmentTimeRatio()), p.Notes)
	}

	fmt.Fprintf(&b, "\nUse compare_roast_batches to see what moved between two of them.\n")
	return b.String(), nil
}

func (s *Set) compareRoastBatches(ctx context.Context, raw json.RawMessage) (string, error) {
	var args struct {
		Coffee string `json:"coffee"`
		BatchA string `json:"batch_a"`
		BatchB string `json:"batch_b"`
	}
	if err := decode(raw, &args); err != nil {
		return "", err
	}

	coffee, err := s.store.FindCoffee(ctx, args.Coffee)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(strings.TrimSpace(args.BatchA), strings.TrimSpace(args.BatchB)) {
		return "", fmt.Errorf("both labels name the same batch (%s)", args.BatchA)
	}

	earlier, err := s.store.FindRoastProfile(ctx, coffee.ID, args.BatchA)
	if err != nil {
		return "", err
	}
	later, err := s.store.FindRoastProfile(ctx, coffee.ID, args.BatchB)
	if err != nil {
		return "", err
	}

	comparison := brew.CompareProfiles(earlier, later)

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", coffee.Name)
	b.WriteString(comparison.Report(earlier, later))
	return b.String(), nil
}

// clock renders seconds the way a roaster or a barista reads a timer.
func clock(seconds int) string {
	if seconds <= 0 {
		return "0:00"
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
