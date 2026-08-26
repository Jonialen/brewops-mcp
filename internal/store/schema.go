package store

// schema is applied on every open. Each statement is idempotent, so opening an
// existing database is a no-op rather than a migration problem.
const schema = `
CREATE TABLE IF NOT EXISTS coffees (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL UNIQUE,
    origin      TEXT    NOT NULL,
    region      TEXT    NOT NULL DEFAULT '',
    variety     TEXT    NOT NULL DEFAULT '',
    process     TEXT    NOT NULL,
    roast_level TEXT    NOT NULL,
    roast_date  TEXT    NOT NULL DEFAULT '',
    altitude    INTEGER NOT NULL DEFAULT 0,
    notes       TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS methods (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT    NOT NULL UNIQUE,
    kind TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS recipes (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    coffee_id          INTEGER NOT NULL REFERENCES coffees(id) ON DELETE CASCADE,
    method_id          INTEGER NOT NULL REFERENCES methods(id) ON DELETE CASCADE,
    ratio              REAL    NOT NULL,
    water_temp_c       REAL    NOT NULL,
    grind_microns      INTEGER NOT NULL DEFAULT 0,
    grind_label        TEXT    NOT NULL DEFAULT '',
    bloom_ratio        REAL    NOT NULL DEFAULT 0,
    bloom_seconds      INTEGER NOT NULL DEFAULT 0,
    target_min_seconds INTEGER NOT NULL DEFAULT 0,
    target_max_seconds INTEGER NOT NULL DEFAULT 0,
    notes              TEXT    NOT NULL DEFAULT '',
    UNIQUE (coffee_id, method_id)
);

CREATE TABLE IF NOT EXISTS roast_profiles (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    coffee_id          INTEGER NOT NULL REFERENCES coffees(id) ON DELETE CASCADE,
    batch              TEXT    NOT NULL,
    roasted_on         TEXT    NOT NULL DEFAULT '',
    charge_temp_c      REAL    NOT NULL DEFAULT 0,
    charge_grams       REAL    NOT NULL DEFAULT 0,
    turning_point_sec  INTEGER NOT NULL DEFAULT 0,
    dry_end_sec        INTEGER NOT NULL DEFAULT 0,
    first_crack_sec    INTEGER NOT NULL DEFAULT 0,
    first_crack_temp_c REAL    NOT NULL DEFAULT 0,
    drop_sec           INTEGER NOT NULL DEFAULT 0,
    drop_temp_c        REAL    NOT NULL DEFAULT 0,
    notes              TEXT    NOT NULL DEFAULT '',
    UNIQUE (coffee_id, batch)
);

CREATE TABLE IF NOT EXISTS extractions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    coffee_id     INTEGER NOT NULL REFERENCES coffees(id) ON DELETE CASCADE,
    method_id     INTEGER NOT NULL REFERENCES methods(id) ON DELETE CASCADE,
    brewed_at     TEXT    NOT NULL,
    dose_grams    REAL    NOT NULL,
    water_grams   REAL    NOT NULL,
    seconds       INTEGER NOT NULL,
    temp_c        REAL    NOT NULL DEFAULT 0,
    grind_label   TEXT    NOT NULL DEFAULT '',
    grind_microns INTEGER NOT NULL DEFAULT 0,
    tds           REAL    NOT NULL DEFAULT 0,
    rating        INTEGER NOT NULL DEFAULT 0,
    notes         TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_extractions_coffee ON extractions (coffee_id, method_id, brewed_at DESC);
CREATE INDEX IF NOT EXISTS idx_recipes_coffee     ON recipes (coffee_id);
CREATE INDEX IF NOT EXISTS idx_profiles_coffee    ON roast_profiles (coffee_id);
`
