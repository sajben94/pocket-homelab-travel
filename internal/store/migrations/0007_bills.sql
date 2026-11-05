-- Spent costs are bills, as in Nextcloud Cospend: someone on the trip paid
-- (payer_id → trip_people) for some of the trip's people (expense_owers),
-- each owing a share by their weight. Planned costs stay budget only.
-- deleted = in the trash bin; updated_ts (unix seconds) lets Cospend clients
-- (MoneyBuster) sync what changed; repeat is Cospend's n|d|w|b|s|m|y.
ALTER TABLE expenses ADD COLUMN payer_id          INTEGER REFERENCES trip_people(id) ON DELETE SET NULL;
ALTER TABLE expenses ADD COLUMN payment_mode_id   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE expenses ADD COLUMN bill_time         TEXT NOT NULL DEFAULT '';
ALTER TABLE expenses ADD COLUMN deleted           INTEGER NOT NULL DEFAULT 0;
ALTER TABLE expenses ADD COLUMN updated_ts        INTEGER NOT NULL DEFAULT 0;
ALTER TABLE expenses ADD COLUMN repeat            TEXT NOT NULL DEFAULT 'n';
ALTER TABLE expenses ADD COLUMN repeat_all_active INTEGER NOT NULL DEFAULT 0;
ALTER TABLE expenses ADD COLUMN repeat_until      TEXT NOT NULL DEFAULT '';
ALTER TABLE expenses ADD COLUMN repeat_freq       INTEGER NOT NULL DEFAULT 1;
UPDATE expenses SET updated_ts = CAST(strftime('%s', updated_at) AS INTEGER);

CREATE TABLE IF NOT EXISTS expense_owers (
  expense_id INTEGER NOT NULL REFERENCES expenses(id) ON DELETE CASCADE,
  member_id  INTEGER NOT NULL REFERENCES trip_people(id) ON DELETE CASCADE,
  PRIMARY KEY (expense_id, member_id)
);
CREATE INDEX IF NOT EXISTS expense_owers_member ON expense_owers(member_id);

-- What was spent before is the owner's: paid by them, for them.
UPDATE expenses SET payer_id = (
  SELECT tp.id FROM trip_people tp JOIN trips t ON t.id = tp.trip_id JOIN people p ON p.id = tp.person_id
  WHERE tp.trip_id = expenses.trip_id AND p.user_id = t.owner_id LIMIT 1)
WHERE kind = 'actual';
INSERT OR IGNORE INTO expense_owers (expense_id, member_id)
  SELECT id, payer_id FROM expenses WHERE kind = 'actual' AND payer_id IS NOT NULL;

-- Cospend's per-project lists. key ties a category to the expense category
-- names this app already uses (food, lodging, …); "reimbursement" is
-- Cospend's special category for paying each other back.
CREATE TABLE IF NOT EXISTS trip_categories (
  id         INTEGER PRIMARY KEY,
  trip_id    INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
  key        TEXT NOT NULL,
  name       TEXT NOT NULL,
  icon       TEXT NOT NULL DEFAULT '',
  color      TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0,
  UNIQUE (trip_id, key)
);
CREATE TABLE IF NOT EXISTS trip_payment_modes (
  id         INTEGER PRIMARY KEY,
  trip_id    INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  icon       TEXT NOT NULL DEFAULT '',
  color      TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0,
  old_id     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS trip_payment_modes_trip ON trip_payment_modes(trip_id);
-- Extra currencies of a trip; exchange_rate = how many trip-currency units one of it is worth (Cospend's meaning).
CREATE TABLE IF NOT EXISTS trip_currencies (
  id            INTEGER PRIMARY KEY,
  trip_id       INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
  name          TEXT NOT NULL,
  exchange_rate REAL NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS trip_currencies_trip ON trip_currencies(trip_id);

-- The Cospend share link of a trip (for MoneyBuster and other Cospend
-- clients): token + optional password; access_level is Cospend's
-- 1 viewer · 2 participant · 3 maintainer · 4 admin.
CREATE TABLE IF NOT EXISTS trip_shares (
  trip_id      INTEGER PRIMARY KEY REFERENCES trips(id) ON DELETE CASCADE,
  token        TEXT NOT NULL UNIQUE,
  password     TEXT NOT NULL DEFAULT '',
  access_level INTEGER NOT NULL DEFAULT 3,
  label        TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
