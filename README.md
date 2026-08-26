# BrewOps MCP Server

An MCP server that holds a speciality coffee shop's own knowledge: its
catalogue, the recipes it has settled on for each lot, the roast profiles behind
them, and what happened at the counter.

It exists so a model does not have to invent numbers. Asked to scale a recipe or
to explain why a brew ran fast, a language model produces figures that look
right; a shop that needs the same cup twice cannot use figures that look right.
Every tool here computes from the shop's records instead.

The Model Context Protocol is implemented directly over JSON-RPC 2.0, with no
MCP SDK.

## Installing

The server is a single static binary with no runtime and no C toolchain.

```sh
git clone https://github.com/Jonialen/brewops-mcp
cd brewops-mcp
go build -o brewops .
```

Or, with Go installed:

```sh
go install github.com/Jonialen/brewops-mcp@latest
```

Requires Go 1.24 or newer to build. Nothing to install to run.

## Adding it to a host

The server speaks MCP over stdio. In a Claude Desktop style configuration:

```json
{
  "mcpServers": {
    "brewops": {
      "command": "/absolute/path/to/brewops"
    }
  }
}
```

On first run it creates and seeds a database with a working catalogue, so the
tools have something to answer with immediately. Pass `-db` to put it elsewhere:

```sh
brewops -db ./shop.db
```

| Flag | Meaning |
| --- | --- |
| `-db` | Path to the shop's database. Created and seeded if absent. Defaults to `$XDG_CONFIG_HOME/brewops/brewops.db`. |

Diagnostics go to stderr. Stdout carries protocol frames and nothing else.

## Protocol

| | |
| --- | --- |
| Transport | stdio, newline-delimited JSON-RPC 2.0 |
| Protocol version | `2025-06-18`, and it echoes back an earlier one a client asks for |
| Capabilities | `tools` |
| Methods | `initialize`, `notifications/initialized`, `tools/list`, `tools/call` |

A tool that ran and failed comes back as a **successful** response whose result
carries `isError: true`, so the model can read the failure and try something
else. Only a protocol fault — an unknown method, an unknown tool, malformed
parameters — comes back as a JSON-RPC error.

## Tools

| Tool | What it does |
| --- | --- |
| `list_coffees` | The whole catalogue: origin, process, roast, notes, days off roast, and which methods have a recipe |
| `get_coffee` | One lot in full. The name may be partial: `Guji` finds `Ethiopia Guji Uraga` |
| `get_recipe` | The shop's recorded recipe for one coffee on one method |
| `scale_recipe` | Works a recipe out for an amount and returns a full brew card with the pour schedule |
| `diagnose_extraction` | Compares a brew against its recipe and recommends the single change to make next |
| `recommend_coffee` | Matches a customer's request against what is actually in stock |
| `record_extraction` | Saves a brew, building a history for each pairing |
| `list_roast_batches` | The recorded batches of one coffee, with development time and ratio |
| `compare_roast_batches` | Compares two batches landmark by landmark and names what most likely changed the cup |

### `scale_recipe`

```json
{
  "coffee": "Guji",
  "method": "V60",
  "water_grams": 350
}
```

Give either `water_grams` or `dose_grams`, never both: the shop's ratio fixes
whichever one is left out.

```
Ethiopia Guji Uraga on V60
  dose        21.0 g
  water       350 g
  ratio       1:16.7
  temperature 94 °C
  grind       medium-fine (~650 µm)
  pours
    bloom         41.9 g at 0:00 (total 42 g)
    first pour   154.1 g at 0:45 (total 196 g)
    second pour  154.0 g at 1:15 (total 350 g)
  target time 2:45–3:10
  rest        12 days off roast (peak)
```

### `diagnose_extraction`

```json
{
  "coffee": "Guji",
  "method": "V60",
  "seconds": 130,
  "dose_grams": 21,
  "water_grams": 350,
  "temp_c": 94,
  "taste_notes": "thin and sour"
}
```

```
measured against the recipe:
  extraction time    expected 2:45–3:10   actual 2:10   35s fast (minor)
  ratio              expected 1:16.7      actual 1:16.7 as written (on target)
  water temperature  expected 94 °C       actual 94 °C  as written (on target)

change one thing: grind one step finer and leave dose, water and temperature
exactly where they are
  why: the brew was 35s fast: water ran through the bed too quickly to dissolve
  what it should have. Grind is the strongest lever on a pour-over, so move it
  alone and the next cup will say whether it was enough
```

The recommendation deliberately changes **one variable at a time**. Changing two
makes the next cup uninterpretable: whichever way it moves, nothing says which
change did it. A second problem is reported under "leave alone this round"
rather than acted on.

Supplying a refractometer reading as `tds` adds an extraction yield, computed
from the beverage mass rather than the water poured — the bed keeps roughly two
grams per gram of coffee, and counting that water inflates the figure.

### `compare_roast_batches`

```json
{
  "coffee": "Guji",
  "batch_a": "L-2401",
  "batch_b": "L-2408"
}
```

```
  landmark                   L-2401     L-2408     change       significance
  turning point              1:00       1:02       +2s          on target
  dry end                    4:30       4:32       +2s          on target
  maillard                   4:30       5:28       +58s         major
  first crack                9:00       10:00      +60s         major
  development                2:00       2:30       +30s         minor
  total roast                11:00      12:30      +90s         major
  development time ratio     18.2%      20.0%      +1.8 points  minor

maillard moved most (+58s, major): a longer browning phase builds body and
caramel sweetness at the cost of the brighter, more delicate notes
```

### `recommend_coffee`

```json
{
  "notes": ["floral", "pronounced acidity"],
  "method": "V60",
  "limit": 3
}
```

Requests are matched against the shop's own vocabulary through a flavour
hierarchy, so asking for *floral* finds a lot the roaster described as *jasmine,
bergamot*. Recommendations come only from coffees actually in stock, and only
from those with a recipe for the method asked for.

## What it computes

- **Scaling** derives a dose from the shop's ratio and builds a pour schedule
  whose stages sum exactly to the number the scale will show.
- **Diagnosis** grades every variable against the recipe's own window, and falls
  outside that window is never graded "on target" however small the miss looks.
- **Extraction yield** uses beverage mass, not water poured.
- **Development time ratio** is what roasters compare batches on, because it
  survives a change in total roast time.
- **Rest windows** follow roast level: a dark roast degasses faster and fades
  sooner, so the same day off roast means something different for each.

## Running the tests

```sh
go test ./... -race
```

## Licence

Academic use. Built for CC3067 Redes, Universidad del Valle de Guatemala.
