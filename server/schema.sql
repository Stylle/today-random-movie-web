CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    username   TEXT NOT NULL UNIQUE COLLATE NOCASE,
    pwd        TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT 'admin',
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf       TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS movies (
    id          TEXT PRIMARY KEY,
    source_id   TEXT NOT NULL,
    title       TEXT NOT NULL,
    year        TEXT NOT NULL DEFAULT '',
    genres      TEXT NOT NULL DEFAULT '[]',
    countries   TEXT NOT NULL DEFAULT '[]',
    overview    TEXT NOT NULL DEFAULT '',
    cover_path  TEXT NOT NULL DEFAULT '',
    tmdb_id     TEXT NOT NULL DEFAULT '',
    tmdb_poster_path TEXT NOT NULL DEFAULT '',
    metadata_source TEXT NOT NULL DEFAULT 'fnos',
    library_id  TEXT NOT NULL DEFAULT '',
    library     TEXT NOT NULL DEFAULT '',
    watched     INTEGER NOT NULL DEFAULT 0,
    duration    TEXT NOT NULL DEFAULT '',
    rating      TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_movies_library ON movies(library_id);
CREATE INDEX IF NOT EXISTS idx_movies_year ON movies(year);
CREATE INDEX IF NOT EXISTS idx_movies_title ON movies(title COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS random_history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    movie_id    TEXT NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
    source      TEXT NOT NULL,
    schedule_id TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_random_history_created ON random_history(created_at DESC);

CREATE TABLE IF NOT EXISTS schedules (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    enabled       INTEGER NOT NULL DEFAULT 1,
    kind          TEXT NOT NULL,
    time_of_day   TEXT NOT NULL,
    timezone      TEXT NOT NULL,
    weekday       INTEGER NOT NULL DEFAULT 1,
    day_of_month  INTEGER NOT NULL DEFAULT 1,
    filters_json  TEXT NOT NULL DEFAULT '{}',
    last_run_key  TEXT NOT NULL DEFAULT '',
    last_run_at   TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS webhook_logs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    event        TEXT NOT NULL,
    schedule_id  TEXT NOT NULL DEFAULT '',
    movie_id     TEXT NOT NULL DEFAULT '',
    ok           INTEGER NOT NULL,
    status_code  INTEGER NOT NULL DEFAULT 0,
    error        TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_webhook_logs_created ON webhook_logs(created_at DESC);
