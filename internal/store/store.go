// Package store persists what a shop knows: its catalogue, the recipes it has
// settled on, the roasts behind them, and what happened at the counter.
//
// It uses SQLite through a pure-Go driver, so the server builds into a single
// binary with no C toolchain and no runtime to install. A classmate who wants
// to run it downloads one file.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Jonialen/brewops-mcp/internal/brew"
)

// dateLayout is how dates are stored. SQLite has no date type, and a sortable
// text format is what lets a plain string comparison order them correctly.
const dateLayout = "2006-01-02"

// Store is the shop's records.
type Store struct {
	db *sql.DB
}

// Open prepares a database at path, creating and seeding it if it is new.
//
// The path ":memory:" gives a database that lives only as long as the process,
// which is what the tests use.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}

	// SQLite defaults to foreign keys off, which quietly turns every reference
	// in the schema into a suggestion.
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: enable foreign keys: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: apply schema: %w", err)
	}

	s := &Store{db: db}
	if err := s.seedIfEmpty(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// Coffees returns the whole catalogue, in the order a menu would list it.
func (s *Store) Coffees(ctx context.Context) ([]brew.Coffee, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, origin, region, variety, process, roast_level, roast_date, altitude, notes
		FROM coffees ORDER BY origin, name`)
	if err != nil {
		return nil, fmt.Errorf("store: list coffees: %w", err)
	}
	defer rows.Close()

	var coffees []brew.Coffee
	for rows.Next() {
		coffee, err := scanCoffee(rows)
		if err != nil {
			return nil, err
		}
		coffees = append(coffees, coffee)
	}
	return coffees, rows.Err()
}

// FindCoffee looks a lot up by name.
//
// The match is loose because the name reaching this server came from a person
// speaking to a model: somebody asking for "the Guji" means the lot filed as
// "Ethiopia Guji Uraga", and refusing to answer until they type it exactly is
// not what the shop wants.
func (s *Store) FindCoffee(ctx context.Context, name string) (brew.Coffee, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return brew.Coffee{}, fmt.Errorf("no coffee name given")
	}

	const query = `
		SELECT id, name, origin, region, variety, process, roast_level, roast_date, altitude, notes
		FROM coffees
		WHERE name = ? COLLATE NOCASE
		   OR name LIKE '%' || ? || '%' COLLATE NOCASE
		ORDER BY
			CASE WHEN name = ? COLLATE NOCASE THEN 0 ELSE 1 END,
			LENGTH(name)
		LIMIT 2`

	rows, err := s.db.QueryContext(ctx, query, name, name, name)
	if err != nil {
		return brew.Coffee{}, fmt.Errorf("store: find coffee %q: %w", name, err)
	}
	defer rows.Close()

	var matches []brew.Coffee
	for rows.Next() {
		coffee, err := scanCoffee(rows)
		if err != nil {
			return brew.Coffee{}, err
		}
		matches = append(matches, coffee)
	}
	if err := rows.Err(); err != nil {
		return brew.Coffee{}, err
	}

	switch len(matches) {
	case 0:
		return brew.Coffee{}, fmt.Errorf("no coffee in the catalogue matches %q", name)
	case 1:
		return matches[0], nil
	default:
		// An exact match sorts first, so two results with no exact match among
		// them is genuinely ambiguous and worth saying so.
		if strings.EqualFold(matches[0].Name, name) {
			return matches[0], nil
		}
		return brew.Coffee{}, fmt.Errorf("%q matches more than one coffee: %s and %s",
			name, matches[0].Name, matches[1].Name)
	}
}

// FindMethod looks a brewing method up by name.
func (s *Store) FindMethod(ctx context.Context, name string) (brew.Method, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return brew.Method{}, fmt.Errorf("no method name given")
	}

	var method brew.Method
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, kind FROM methods
		WHERE name = ? COLLATE NOCASE OR REPLACE(name, ' ', '') = REPLACE(?, ' ', '') COLLATE NOCASE
		LIMIT 1`, name, name).Scan(&method.ID, &method.Name, &method.Kind)

	if err == sql.ErrNoRows {
		return brew.Method{}, fmt.Errorf("no brewing method called %q is set up", name)
	}
	if err != nil {
		return brew.Method{}, fmt.Errorf("store: find method %q: %w", name, err)
	}
	return method, nil
}

// Methods returns every brewing method the shop has set up.
func (s *Store) Methods(ctx context.Context) ([]brew.Method, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, kind FROM methods ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("store: list methods: %w", err)
	}
	defer rows.Close()

	var methods []brew.Method
	for rows.Next() {
		var m brew.Method
		if err := rows.Scan(&m.ID, &m.Name, &m.Kind); err != nil {
			return nil, err
		}
		methods = append(methods, m)
	}
	return methods, rows.Err()
}

// Recipe returns the shop's recipe for one coffee on one method.
func (s *Store) Recipe(ctx context.Context, coffeeID, methodID int64) (brew.Recipe, error) {
	var r brew.Recipe
	err := s.db.QueryRowContext(ctx, `
		SELECT id, coffee_id, method_id, ratio, water_temp_c, grind_microns, grind_label,
		       bloom_ratio, bloom_seconds, target_min_seconds, target_max_seconds, notes
		FROM recipes WHERE coffee_id = ? AND method_id = ?`, coffeeID, methodID).
		Scan(&r.ID, &r.CoffeeID, &r.MethodID, &r.Ratio, &r.WaterTempC, &r.GrindMicrons,
			&r.GrindLabel, &r.BloomRatio, &r.BloomSeconds, &r.TargetMinSeconds,
			&r.TargetMaxSeconds, &r.Notes)

	if err == sql.ErrNoRows {
		return brew.Recipe{}, errNoRecipe
	}
	if err != nil {
		return brew.Recipe{}, fmt.Errorf("store: load recipe: %w", err)
	}
	return r, nil
}

// errNoRecipe marks the absence of a recipe, so a caller can say which coffee
// and method it was looking for rather than repeating ids back at the user.
var errNoRecipe = fmt.Errorf("no recipe recorded")

// IsNoRecipe reports whether an error means the pairing has no recipe.
func IsNoRecipe(err error) bool { return err == errNoRecipe }

// MethodsFor returns the methods a coffee has a recipe for.
func (s *Store) MethodsFor(ctx context.Context, coffeeID int64) ([]brew.Method, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.name, m.kind FROM methods m
		JOIN recipes r ON r.method_id = m.id
		WHERE r.coffee_id = ? ORDER BY m.name`, coffeeID)
	if err != nil {
		return nil, fmt.Errorf("store: list methods for coffee: %w", err)
	}
	defer rows.Close()

	var methods []brew.Method
	for rows.Next() {
		var m brew.Method
		if err := rows.Scan(&m.ID, &m.Name, &m.Kind); err != nil {
			return nil, err
		}
		methods = append(methods, m)
	}
	return methods, rows.Err()
}

// RoastProfiles returns every recorded batch of a coffee, oldest first.
func (s *Store) RoastProfiles(ctx context.Context, coffeeID int64) ([]brew.RoastProfile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, coffee_id, batch, charge_temp_c, charge_grams, turning_point_sec,
		       dry_end_sec, first_crack_sec, first_crack_temp_c, drop_sec, drop_temp_c, notes
		FROM roast_profiles WHERE coffee_id = ? ORDER BY roasted_on, batch`, coffeeID)
	if err != nil {
		return nil, fmt.Errorf("store: list roast profiles: %w", err)
	}
	defer rows.Close()

	var profiles []brew.RoastProfile
	for rows.Next() {
		var p brew.RoastProfile
		if err := rows.Scan(&p.ID, &p.CoffeeID, &p.Batch, &p.ChargeTempC, &p.ChargeGrams,
			&p.TurningPointSec, &p.DryEndSec, &p.FirstCrackSec, &p.FirstCrackTempC,
			&p.DropSec, &p.DropTempC, &p.Notes); err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// FindRoastProfile looks a single batch up by its label.
func (s *Store) FindRoastProfile(ctx context.Context, coffeeID int64, batch string) (brew.RoastProfile, error) {
	profiles, err := s.RoastProfiles(ctx, coffeeID)
	if err != nil {
		return brew.RoastProfile{}, err
	}
	for _, p := range profiles {
		if strings.EqualFold(p.Batch, strings.TrimSpace(batch)) {
			return p, nil
		}
	}

	labels := make([]string, 0, len(profiles))
	for _, p := range profiles {
		labels = append(labels, p.Batch)
	}
	return brew.RoastProfile{}, fmt.Errorf("no batch %q for this coffee; recorded batches are %s",
		batch, strings.Join(labels, ", "))
}

// RecordExtraction saves what happened at the counter.
func (s *Store) RecordExtraction(
	ctx context.Context,
	coffeeID, methodID int64,
	actual brew.Extraction,
	rating int,
	at time.Time,
) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO extractions
			(coffee_id, method_id, brewed_at, dose_grams, water_grams, seconds, temp_c,
			 grind_label, grind_microns, tds, rating, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		coffeeID, methodID, at.UTC().Format(time.RFC3339), actual.DoseGrams, actual.WaterGrams,
		actual.Seconds, actual.TempC, actual.GrindLabel, actual.GrindMicrons, actual.TDS,
		rating, actual.Notes)
	if err != nil {
		return fmt.Errorf("store: record extraction: %w", err)
	}
	return nil
}

// RecentExtractions returns the last brews of one coffee on one method.
func (s *Store) RecentExtractions(ctx context.Context, coffeeID, methodID int64, limit int) ([]RecordedExtraction, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT brewed_at, dose_grams, water_grams, seconds, temp_c, grind_label,
		       grind_microns, tds, rating, notes
		FROM extractions WHERE coffee_id = ? AND method_id = ?
		ORDER BY brewed_at DESC LIMIT ?`, coffeeID, methodID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list extractions: %w", err)
	}
	defer rows.Close()

	var out []RecordedExtraction
	for rows.Next() {
		var (
			rec    RecordedExtraction
			brewed string
		)
		if err := rows.Scan(&brewed, &rec.DoseGrams, &rec.WaterGrams, &rec.Seconds,
			&rec.TempC, &rec.GrindLabel, &rec.GrindMicrons, &rec.TDS,
			&rec.Rating, &rec.Notes); err != nil {
			return nil, err
		}
		rec.BrewedAt, _ = time.Parse(time.RFC3339, brewed)
		out = append(out, rec)
	}
	return out, rows.Err()
}

// RecordedExtraction is a saved brew, with when it happened and how it was rated.
type RecordedExtraction struct {
	brew.Extraction
	BrewedAt time.Time
	Rating   int
}

type scanner interface {
	Scan(dest ...any) error
}

func scanCoffee(row scanner) (brew.Coffee, error) {
	var (
		c         brew.Coffee
		roastDate string
		notes     string
	)
	if err := row.Scan(&c.ID, &c.Name, &c.Origin, &c.Region, &c.Variety, &c.Process,
		&c.Roast, &roastDate, &c.Altitude, &notes); err != nil {
		return brew.Coffee{}, fmt.Errorf("store: read coffee: %w", err)
	}

	if roastDate != "" {
		c.RoastOn, _ = time.Parse(dateLayout, roastDate)
	}
	if notes != "" {
		c.Notes = strings.Split(notes, ",")
		for i := range c.Notes {
			c.Notes[i] = strings.TrimSpace(c.Notes[i])
		}
	}
	return c, nil
}
