// Package tools exposes the shop's records and reasoning as MCP tools.
//
// Every tool here computes or looks something up. None of them asks the model
// to supply a number it could have worked out: that is the whole argument for
// the server existing, because a model asked to scale a recipe produces figures
// that look right, and a shop that needs the same cup twice cannot use figures
// that look right.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Jonialen/brewops-mcp/internal/brew"
	"github.com/Jonialen/brewops-mcp/internal/server"
	"github.com/Jonialen/brewops-mcp/internal/store"
)

// Clock supplies the current time. It is a field rather than a call to
// time.Now so that rest windows and recorded brews can be tested against a
// fixed day.
type Clock func() time.Time

// Set wires the shop's records to the tools that read them.
type Set struct {
	store *store.Store
	now   Clock
}

// New returns a tool set backed by the given store.
func New(s *store.Store, now Clock) *Set {
	if now == nil {
		now = time.Now
	}
	return &Set{store: s, now: now}
}

// Register adds every tool to a server.
func (s *Set) Register(srv *server.Server) {
	for _, tool := range s.Tools() {
		srv.Register(tool)
	}
}

// Tools returns the published tools.
func (s *Set) Tools() []server.Tool {
	return []server.Tool{
		{
			Name:        "list_coffees",
			Title:       "List Coffees",
			Description: "List every coffee currently in the shop's catalogue, with its origin, process, roast level, sensory notes, how many days it is off roast, and which brewing methods have a recipe. Use this to see what is actually available before recommending anything.",
			InputSchema: object(nil, nil),
			Handler:     s.listCoffees,
		},
		{
			Name:        "get_coffee",
			Title:       "Get Coffee",
			Description: "Look one coffee up by name and return everything the shop knows about it, including which methods it has recipes for. The name may be partial: 'Guji' finds 'Ethiopia Guji Uraga'.",
			InputSchema: object(map[string]any{
				"name": field("string", "Coffee name, whole or partial."),
			}, []string{"name"}),
			Handler: s.getCoffee,
		},
		{
			Name:        "get_recipe",
			Title:       "Get Recipe",
			Description: "Return the shop's recorded recipe for one coffee on one brewing method: ratio, water temperature, grind, bloom and the expected extraction window. Use this instead of recalling a general recipe, because these are the numbers this shop has settled on for this lot.",
			InputSchema: object(map[string]any{
				"coffee": field("string", "Coffee name, whole or partial."),
				"method": field("string", "Brewing method, such as V60, Chemex, AeroPress, French Press or Espresso."),
			}, []string{"coffee", "method"}),
			Handler: s.getRecipe,
		},
		{
			Name:        "scale_recipe",
			Title:       "Scale Recipe",
			Description: "Work a recipe out for a specific amount and return a full brew card: dose, water, bloom and the pour schedule with running totals. Give either the water you want to brew or the dose you have; the server derives the other from the shop's ratio. Always use this rather than calculating the dose yourself.",
			InputSchema: object(map[string]any{
				"coffee":      field("string", "Coffee name, whole or partial."),
				"method":      field("string", "Brewing method."),
				"water_grams": field("number", "Grams of water to brew. Give this or dose_grams, not both."),
				"dose_grams":  field("number", "Grams of coffee available. Give this or water_grams, not both."),
			}, []string{"coffee", "method"}),
			Handler: s.scaleRecipe,
		},
		{
			Name:        "diagnose_extraction",
			Title:       "Diagnose Extraction",
			Description: "Compare a brew that was actually made against the shop's recipe, report every variable that fell outside its window, and recommend the single change to make next. Use this whenever a barista describes a brew that did not come out as expected. The recommendation deliberately changes one variable at a time, because changing two makes the next cup impossible to interpret.",
			InputSchema: object(map[string]any{
				"coffee":      field("string", "Coffee name, whole or partial."),
				"method":      field("string", "Brewing method."),
				"seconds":     field("integer", "How long the extraction took, in seconds."),
				"dose_grams":  field("number", "Grams of coffee used."),
				"water_grams": field("number", "Grams of water used."),
				"temp_c":      field("number", "Water temperature in Celsius, if it was measured."),
				"grind_label": field("string", "Grind setting used, if noted."),
				"tds":         field("number", "Refractometer reading as a percentage, if one was taken. Optional; supplying it adds an extraction yield."),
				"taste_notes": field("string", "What the barista tasted, in their own words."),
			}, []string{"coffee", "method", "seconds", "dose_grams", "water_grams"}),
			Handler: s.diagnoseExtraction,
		},
		{
			Name:        "recommend_coffee",
			Title:       "Recommend Coffee",
			Description: "Find the coffees in the shop's catalogue that best match a customer's request, optionally restricted to those with a recipe for a given method. Recommendations come only from what the shop actually has in stock.",
			InputSchema: object(map[string]any{
				"notes":  arrayField("string", "Flavour descriptors the customer asked for, such as floral, fruity, chocolate or bright."),
				"method": field("string", "Restrict to coffees that have a recipe for this method. Optional."),
				"limit":  field("integer", "How many suggestions to return. Defaults to 3."),
			}, []string{"notes"}),
			Handler: s.recommendCoffee,
		},
		{
			Name:        "record_extraction",
			Title:       "Record Extraction",
			Description: "Save a brew that was made, so the shop builds a history of what has been tried on each lot. Use this after a barista reports a result they want kept.",
			InputSchema: object(map[string]any{
				"coffee":      field("string", "Coffee name, whole or partial."),
				"method":      field("string", "Brewing method."),
				"seconds":     field("integer", "How long the extraction took."),
				"dose_grams":  field("number", "Grams of coffee used."),
				"water_grams": field("number", "Grams of water used."),
				"temp_c":      field("number", "Water temperature in Celsius."),
				"grind_label": field("string", "Grind setting used."),
				"rating":      field("integer", "How the cup was rated, from 1 to 10."),
				"notes":       field("string", "What the barista tasted."),
			}, []string{"coffee", "method", "seconds", "dose_grams", "water_grams"}),
			Handler: s.recordExtraction,
		},
		{
			Name:        "list_roast_batches",
			Title:       "List Roast Batches",
			Description: "List the recorded roast batches for one coffee, with the landmarks of each: first crack, drop, development time and development time ratio. Use this before comparing two batches, to find out which labels exist.",
			InputSchema: object(map[string]any{
				"coffee": field("string", "Coffee name, whole or partial."),
			}, []string{"coffee"}),
			Handler: s.listRoastBatches,
		},
		{
			Name:        "compare_roast_batches",
			Title:       "Compare Roast Batches",
			Description: "Compare two roast batches of the same coffee landmark by landmark, grade how significant each difference is, and name the change most likely to explain a difference in the cup. Use this when a roaster reports that a new batch of a familiar coffee is tasting different.",
			InputSchema: object(map[string]any{
				"coffee":  field("string", "Coffee name, whole or partial."),
				"batch_a": field("string", "The earlier batch label, used as the reference."),
				"batch_b": field("string", "The later batch label, the one being questioned."),
			}, []string{"coffee", "batch_a", "batch_b"}),
			Handler: s.compareRoastBatches,
		},
	}
}

// -- schema helpers ------------------------------------------------------

func object(properties map[string]any, required []string) json.RawMessage {
	if properties == nil {
		properties = map[string]any{}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}

	raw, err := json.Marshal(schema)
	if err != nil {
		// The schemas are literals in this file; a failure here is a typo
		// caught the first time the program runs.
		panic("tools: build schema: " + err.Error())
	}
	return raw
}

func field(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

func arrayField(itemKind, description string) map[string]any {
	return map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": itemKind},
		"description": description,
	}
}

// -- shared lookups ------------------------------------------------------

// pairing resolves the coffee and method named in a call, along with the recipe
// joining them.
func (s *Set) pairing(ctx context.Context, coffeeName, methodName string) (brew.Coffee, brew.Method, brew.Recipe, error) {
	coffee, err := s.store.FindCoffee(ctx, coffeeName)
	if err != nil {
		return brew.Coffee{}, brew.Method{}, brew.Recipe{}, err
	}
	method, err := s.store.FindMethod(ctx, methodName)
	if err != nil {
		return brew.Coffee{}, brew.Method{}, brew.Recipe{}, err
	}

	recipe, err := s.store.Recipe(ctx, coffee.ID, method.ID)
	if store.IsNoRecipe(err) {
		// Naming the methods that do exist turns a dead end into a choice.
		available, listErr := s.store.MethodsFor(ctx, coffee.ID)
		if listErr != nil || len(available) == 0 {
			return coffee, method, brew.Recipe{}, fmt.Errorf(
				"%s has no recipe for %s, and none for any other method either",
				coffee.Name, method.Name)
		}
		return coffee, method, brew.Recipe{}, fmt.Errorf(
			"%s has no recipe for %s; it does have one for %s",
			coffee.Name, method.Name, joinMethods(available))
	}
	if err != nil {
		return coffee, method, brew.Recipe{}, err
	}
	return coffee, method, recipe, nil
}

func joinMethods(methods []brew.Method) string {
	names := make([]string, 0, len(methods))
	for _, m := range methods {
		names = append(names, m.Name)
	}
	sort.Strings(names)

	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

func decode(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("could not read the arguments: %w", err)
	}
	return nil
}
