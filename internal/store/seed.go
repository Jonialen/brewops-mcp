package store

import (
	"context"
	"fmt"
	"time"
)

// seedIfEmpty fills a new database with a shop's worth of records.
//
// The data is a working catalogue rather than placeholders, because the tools
// are only worth anything against realistic numbers: a ratio, a rest window and
// a roast curve that do not resemble the real thing produce advice that does
// not either.
func (s *Store) seedIfEmpty(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM coffees`).Scan(&count); err != nil {
		return fmt.Errorf("store: check catalogue: %w", err)
	}
	if count > 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin seed: %w", err)
	}
	defer tx.Rollback()

	for _, statement := range seedStatements() {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return fmt.Errorf("store: seed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit seed: %w", err)
	}
	return nil
}

type seedStatement struct {
	query string
	args  []any
}

// roastedDaysAgo dates a lot relative to today, so a freshly seeded database
// always holds coffees at believable points in their rest window instead of
// lots that went stale whenever this file was written.
func roastedDaysAgo(days int) string {
	return time.Now().AddDate(0, 0, -days).Format(dateLayout)
}

func seedStatements() []seedStatement {
	const insertCoffee = `INSERT INTO coffees
		(name, origin, region, variety, process, roast_level, roast_date, altitude, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	const insertMethod = `INSERT INTO methods (name, kind) VALUES (?, ?)`

	const insertRecipe = `INSERT INTO recipes
		(coffee_id, method_id, ratio, water_temp_c, grind_microns, grind_label,
		 bloom_ratio, bloom_seconds, target_min_seconds, target_max_seconds, notes)
		VALUES (
			(SELECT id FROM coffees WHERE name = ?),
			(SELECT id FROM methods WHERE name = ?),
			?, ?, ?, ?, ?, ?, ?, ?, ?)`

	const insertProfile = `INSERT INTO roast_profiles
		(coffee_id, batch, roasted_on, charge_temp_c, charge_grams, turning_point_sec,
		 dry_end_sec, first_crack_sec, first_crack_temp_c, drop_sec, drop_temp_c, notes)
		VALUES ((SELECT id FROM coffees WHERE name = ?), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	return []seedStatement{
		// -- catalogue ---------------------------------------------------
		{insertCoffee, []any{"Ethiopia Guji Uraga", "Ethiopia", "Guji, Uraga", "Heirloom",
			"washed", "light", roastedDaysAgo(12), 2050,
			"jasmine, bergamot, stone fruit, black tea"}},
		{insertCoffee, []any{"Ethiopia Sidamo Bombe", "Ethiopia", "Sidamo, Bombe", "Heirloom",
			"natural", "light", roastedDaysAgo(9), 2100,
			"blueberry, strawberry, cocoa nib, syrupy"}},
		{insertCoffee, []any{"Colombia Huila Pitalito", "Colombia", "Huila, Pitalito", "Caturra",
			"washed", "medium-light", roastedDaysAgo(16), 1750,
			"red apple, caramel, orange, almond"}},
		{insertCoffee, []any{"Guatemala Huehuetenango La Bolsa", "Guatemala", "Huehuetenango", "Bourbon",
			"washed", "medium", roastedDaysAgo(21), 1900,
			"milk chocolate, green apple, hazelnut, honey"}},
		{insertCoffee, []any{"Kenya Nyeri Gichathaini", "Kenya", "Nyeri", "SL28, SL34",
			"washed", "light", roastedDaysAgo(14), 1800,
			"blackcurrant, grapefruit, tomato, brown sugar"}},
		{insertCoffee, []any{"Brazil Cerrado Fazenda Rio Verde", "Brazil", "Cerrado Mineiro", "Yellow Catuai",
			"natural", "medium-dark", roastedDaysAgo(8), 1150,
			"peanut, dark chocolate, molasses, low acidity"}},

		// -- methods -----------------------------------------------------
		{insertMethod, []any{"V60", "pour-over"}},
		{insertMethod, []any{"Chemex", "pour-over"}},
		{insertMethod, []any{"AeroPress", "immersion"}},
		{insertMethod, []any{"French Press", "immersion"}},
		{insertMethod, []any{"Espresso", "pressure"}},

		// -- recipes -----------------------------------------------------
		// The shop's own worked example: 1:16.7 on the Guji, which is where the
		// 21 g for 350 g in the brief comes from.
		{insertRecipe, []any{"Ethiopia Guji Uraga", "V60",
			16.7, 94.0, 650, "medium-fine", 2.0, 45, 165, 190,
			"Delicate lot. Pour gently; a fast bed here usually means the grind drifted coarse."}},
		{insertRecipe, []any{"Ethiopia Guji Uraga", "Chemex",
			16.0, 94.0, 800, "medium", 2.0, 45, 240, 300,
			"Bigger bed than the V60; expect a longer draw down."}},
		{insertRecipe, []any{"Ethiopia Sidamo Bombe", "V60",
			16.0, 92.0, 700, "medium", 2.0, 45, 150, 180,
			"Naturals extract fast. Lower temperature keeps the ferment in check."}},
		{insertRecipe, []any{"Colombia Huila Pitalito", "V60",
			16.7, 93.0, 650, "medium-fine", 2.0, 40, 165, 195, ""}},
		{insertRecipe, []any{"Colombia Huila Pitalito", "Espresso",
			2.2, 93.0, 250, "fine", 0, 0, 26, 32,
			"18 g in, about 40 g out. Ratio here is beverage out over dose in."}},
		{insertRecipe, []any{"Guatemala Huehuetenango La Bolsa", "V60",
			16.0, 95.0, 700, "medium", 2.0, 45, 170, 200, ""}},
		{insertRecipe, []any{"Guatemala Huehuetenango La Bolsa", "French Press",
			15.0, 95.0, 1100, "coarse", 0, 0, 240, 270,
			"Break the crust at 4:00, skim, then decant."}},
		{insertRecipe, []any{"Kenya Nyeri Gichathaini", "V60",
			16.7, 95.0, 620, "medium-fine", 2.0, 45, 175, 205,
			"Dense bean, high acidity. Hot water and a fine grind keep it from tasting sharp."}},
		{insertRecipe, []any{"Kenya Nyeri Gichathaini", "AeroPress",
			14.0, 88.0, 700, "medium", 0, 0, 120, 150,
			"Inverted, steep two minutes, press slowly over thirty seconds."}},
		{insertRecipe, []any{"Brazil Cerrado Fazenda Rio Verde", "Espresso",
			2.0, 91.0, 240, "fine", 0, 0, 25, 30,
			"18 g in, 36 g out. Lower temperature keeps the roast from turning bitter."}},
		{insertRecipe, []any{"Brazil Cerrado Fazenda Rio Verde", "French Press",
			15.0, 93.0, 1100, "coarse", 0, 0, 240, 270, ""}},

		// -- roast profiles ----------------------------------------------
		// Two batches of the Guji: the second took first crack a minute later
		// and rode it further, which is the shop's fourth case.
		{insertProfile, []any{"Ethiopia Guji Uraga", "L-2401", roastedDaysAgo(40),
			198.0, 12000.0, 60, 270, 540, 196.5, 660, 205.0,
			"Reference roast. Cupped as intended: jasmine forward, clean finish."}},
		{insertProfile, []any{"Ethiopia Guji Uraga", "L-2408", roastedDaysAgo(12),
			198.0, 12000.0, 62, 272, 600, 197.0, 750, 207.5,
			"Cupped heavier and flatter than L-2401. Floral character muted."}},
		{insertProfile, []any{"Kenya Nyeri Gichathaini", "K-2405", roastedDaysAgo(45),
			200.0, 12000.0, 58, 260, 520, 197.0, 640, 206.0, "Reference roast."}},
		{insertProfile, []any{"Kenya Nyeri Gichathaini", "K-2411", roastedDaysAgo(14),
			200.0, 12000.0, 59, 262, 528, 197.5, 648, 206.5,
			"Within tolerance of K-2405."}},
	}
}
