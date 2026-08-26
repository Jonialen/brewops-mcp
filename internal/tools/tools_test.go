package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Jonialen/brewops-mcp/internal/server"
	"github.com/Jonialen/brewops-mcp/internal/store"
)

var brewDay = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

func testSet(t *testing.T) (*Set, context.Context) {
	t.Helper()

	ctx := context.Background()
	shop, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { shop.Close() })

	return New(shop, func() time.Time { return brewDay }), ctx
}

// call runs a tool by name with the given arguments.
func call(t *testing.T, s *Set, ctx context.Context, name string, args map[string]any) (string, error) {
	t.Helper()

	for _, tool := range s.Tools() {
		if tool.Name != name {
			continue
		}
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("encode arguments: %v", err)
		}
		return tool.Handler(ctx, raw)
	}
	t.Fatalf("no tool called %q", name)
	return "", nil
}

// The schema is what the model reads to build a call, so a malformed one is a
// tool nobody can use correctly.
func TestEveryToolPublishesAUsableSchema(t *testing.T) {
	s, _ := testSet(t)

	names := map[string]bool{}
	for _, tool := range s.Tools() {
		t.Run(tool.Name, func(t *testing.T) {
			if names[tool.Name] {
				t.Fatalf("%q is registered twice", tool.Name)
			}
			names[tool.Name] = true

			if tool.Description == "" {
				t.Error("no description, so the model has nothing to choose on")
			}
			if len(tool.Description) < 60 {
				t.Errorf("description is %d characters; too terse to pick from a list of tools",
					len(tool.Description))
			}
			if tool.Handler == nil {
				t.Fatal("no handler")
			}

			var schema map[string]any
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				t.Fatalf("schema is not valid JSON: %v", err)
			}
			if schema["type"] != "object" {
				t.Errorf("schema type = %v, want object", schema["type"])
			}
			properties, ok := schema["properties"].(map[string]any)
			if !ok {
				t.Fatal("schema has no properties object")
			}

			// Every required field has to exist among the properties, or the
			// model is told to send something the schema never describes.
			if required, present := schema["required"].([]any); present {
				for _, field := range required {
					if _, described := properties[field.(string)]; !described {
						t.Errorf("%q is required but not described", field)
					}
				}
			}
			for name, raw := range properties {
				property, ok := raw.(map[string]any)
				if !ok {
					t.Fatalf("property %q is not an object", name)
				}
				if property["description"] == "" || property["description"] == nil {
					t.Errorf("property %q has no description", name)
				}
			}
		})
	}

	if len(names) < 8 {
		t.Errorf("only %d tools are published", len(names))
	}
}

func TestToolsRegisterOnAServer(t *testing.T) {
	s, ctx := testSet(t)

	srv := server.New("brewops", "test")
	s.Register(srv)

	msg := &server.Message{
		JSONRPC: server.Version,
		ID:      json.RawMessage("1"),
		Method:  "tools/list",
	}
	resp := srv.Handle(ctx, msg)
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/list failed: %+v", resp)
	}

	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Tools) != len(s.Tools()) {
		t.Errorf("server published %d tools, the set defines %d",
			len(result.Tools), len(s.Tools()))
	}
}

// The brief's first case, through the tool a host would actually call.
func TestScaleRecipeAnswersTheBriefsExample(t *testing.T) {
	s, ctx := testSet(t)

	out, err := call(t, s, ctx, "scale_recipe", map[string]any{
		"coffee": "Guji", "method": "V60", "water_grams": 350,
	})
	if err != nil {
		t.Fatalf("scale_recipe: %v", err)
	}

	for _, want := range []string{"21.0 g", "350 g", "1:16.7", "94 °C", "2:45–3:10"} {
		if !strings.Contains(out, want) {
			t.Errorf("brew card does not contain %q:\n%s", want, out)
		}
	}
}

// Giving both amounts is a contradiction, and saying so beats picking one.
func TestScaleRecipeRejectsContradictoryAmounts(t *testing.T) {
	s, ctx := testSet(t)

	_, err := call(t, s, ctx, "scale_recipe", map[string]any{
		"coffee": "Guji", "method": "V60", "water_grams": 350, "dose_grams": 30,
	})
	if err == nil {
		t.Fatal("both amounts were accepted")
	}
	if !strings.Contains(err.Error(), "not both") {
		t.Errorf("error = %q", err)
	}

	if _, err := call(t, s, ctx, "scale_recipe", map[string]any{
		"coffee": "Guji", "method": "V60",
	}); err == nil {
		t.Fatal("neither amount was given and it was accepted anyway")
	}
}

// A dead end becomes a choice when the answer names what does exist.
func TestMissingRecipeNamesTheMethodsThatExist(t *testing.T) {
	s, ctx := testSet(t)

	_, err := call(t, s, ctx, "get_recipe", map[string]any{
		"coffee": "Brazil Cerrado", "method": "V60",
	})
	if err == nil {
		t.Fatal("a recipe that was never written was returned")
	}
	if !strings.Contains(err.Error(), "does have one for") {
		t.Errorf("error = %q, want it to name the methods that do exist", err)
	}
}

// The brief's second case, end to end.
func TestDiagnoseExtractionAnswersTheBriefsCase(t *testing.T) {
	s, ctx := testSet(t)

	out, err := call(t, s, ctx, "diagnose_extraction", map[string]any{
		"coffee": "Guji", "method": "V60",
		"seconds": 130, "dose_grams": 21, "water_grams": 350, "temp_c": 94,
		"taste_notes": "thin and sour",
	})
	if err != nil {
		t.Fatalf("diagnose_extraction: %v", err)
	}

	if !strings.Contains(out, "finer") {
		t.Errorf("no finer grind was recommended:\n%s", out)
	}
	if !strings.Contains(out, "change one thing") {
		t.Errorf("the one-variable rule is not stated:\n%s", out)
	}
	if !strings.Contains(out, "thin and sour") {
		t.Errorf("what the barista tasted was dropped:\n%s", out)
	}
}

func TestDiagnoseExtractionRequiresWhatItNeeds(t *testing.T) {
	s, ctx := testSet(t)

	cases := []struct {
		name  string
		args  map[string]any
		wants string
	}{
		{
			"no time",
			map[string]any{"coffee": "Guji", "method": "V60", "dose_grams": 21, "water_grams": 350},
			"extraction time",
		},
		{
			"no dose",
			map[string]any{"coffee": "Guji", "method": "V60", "seconds": 130, "water_grams": 350},
			"ratio actually used",
		},
		{
			"unknown coffee",
			map[string]any{"coffee": "Sumatra", "method": "V60", "seconds": 130, "dose_grams": 21, "water_grams": 350},
			"no coffee in the catalogue",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := call(t, s, ctx, "diagnose_extraction", tc.args)
			if err == nil {
				t.Fatal("the call was accepted")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wants)
			}
		})
	}
}

// The brief's third case: recommendations come only from what is in stock, and
// only from what can be brewed on the method asked for.
func TestRecommendCoffeeStaysWithinTheCatalogue(t *testing.T) {
	s, ctx := testSet(t)

	out, err := call(t, s, ctx, "recommend_coffee", map[string]any{
		"notes": []string{"floral", "pronounced acidity"}, "method": "V60", "limit": 3,
	})
	if err != nil {
		t.Fatalf("recommend_coffee: %v", err)
	}
	if !strings.Contains(out, "Ethiopia Guji Uraga") {
		t.Errorf("the floral, acidic lot was not recommended:\n%s", out)
	}

	// The Brazil is peanut and dark chocolate, and has no V60 recipe either.
	if strings.Contains(out, "Brazil") {
		t.Errorf("a coffee that answers neither the request nor the method was offered:\n%s", out)
	}

	_, err = call(t, s, ctx, "recommend_coffee", map[string]any{
		"notes": []string{"durian", "petrol"},
	})
	if err == nil {
		t.Fatal("a request nothing matches produced a recommendation anyway")
	}
	if !strings.Contains(err.Error(), "nothing in the catalogue") {
		t.Errorf("error = %q", err)
	}
}

// The brief's fourth case.
func TestCompareRoastBatchesExplainsTheDifference(t *testing.T) {
	s, ctx := testSet(t)

	out, err := call(t, s, ctx, "compare_roast_batches", map[string]any{
		"coffee": "Guji", "batch_a": "L-2401", "batch_b": "L-2408",
	})
	if err != nil {
		t.Fatalf("compare_roast_batches: %v", err)
	}

	for _, want := range []string{"first crack", "development", "L-2401", "L-2408"} {
		if !strings.Contains(out, want) {
			t.Errorf("comparison does not mention %q:\n%s", want, out)
		}
	}

	if _, err := call(t, s, ctx, "compare_roast_batches", map[string]any{
		"coffee": "Guji", "batch_a": "L-2401", "batch_b": "L-2401",
	}); err == nil {
		t.Error("a batch was compared against itself")
	}

	_, err = call(t, s, ctx, "compare_roast_batches", map[string]any{
		"coffee": "Guji", "batch_a": "L-2401", "batch_b": "L-9999",
	})
	if err == nil {
		t.Fatal("a batch that does not exist was compared")
	}
	if !strings.Contains(err.Error(), "L-2408") {
		t.Errorf("error = %q, want it to list the batches that do exist", err)
	}
}

func TestRecordExtractionBuildsAHistory(t *testing.T) {
	s, ctx := testSet(t)

	args := map[string]any{
		"coffee": "Guji", "method": "V60",
		"seconds": 130, "dose_grams": 21, "water_grams": 350,
		"temp_c": 94, "grind_label": "medium-fine", "rating": 5,
		"notes": "ran fast, thin",
	}
	if _, err := call(t, s, ctx, "record_extraction", args); err != nil {
		t.Fatalf("record_extraction: %v", err)
	}

	args["seconds"] = 175
	args["rating"] = 9
	args["notes"] = "ground finer, much better"

	out, err := call(t, s, ctx, "record_extraction", args)
	if err != nil {
		t.Fatalf("second record_extraction: %v", err)
	}

	for _, want := range []string{"2:55", "2:10", "9/10", "ground finer"} {
		if !strings.Contains(out, want) {
			t.Errorf("history does not show %q:\n%s", want, out)
		}
	}

	if _, err := call(t, s, ctx, "record_extraction", map[string]any{
		"coffee": "Guji", "method": "V60",
		"seconds": 130, "dose_grams": 21, "water_grams": 350, "rating": 99,
	}); err == nil {
		t.Error("a rating of 99 out of 10 was accepted")
	}
}

func TestListCoffeesShowsRestAndRecipes(t *testing.T) {
	s, ctx := testSet(t)

	out, err := call(t, s, ctx, "list_coffees", nil)
	if err != nil {
		t.Fatalf("list_coffees: %v", err)
	}

	for _, want := range []string{"Ethiopia Guji Uraga", "days off roast", "recipes"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing does not show %q:\n%s", want, out)
		}
	}
}
