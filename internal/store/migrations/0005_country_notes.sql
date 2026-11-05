-- Each person's own notes on a country: corrections to the built-in facts
-- (facts: {"capital": "…"}), facts of their own ([{"label": "Plugs", "value": "C/F"}])
-- and free text.
CREATE TABLE IF NOT EXISTS country_notes (
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code       TEXT NOT NULL,
  facts      TEXT NOT NULL DEFAULT '{}',
  extra      TEXT NOT NULL DEFAULT '[]',
  notes      TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, code)
);
