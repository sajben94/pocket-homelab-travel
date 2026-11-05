-- People you travel with. Everyone keeps their own address book; a person may
-- be linked to an account on this box (then they can be given access to a
-- trip), or be just a name (someone who only shares the bills). is_self marks
-- the book owner's own entry.
CREATE TABLE IF NOT EXISTS people (
  id         INTEGER PRIMARY KEY,
  owner_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  color      TEXT NOT NULL DEFAULT '',
  user_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
  email      TEXT NOT NULL DEFAULT '',
  notes      TEXT NOT NULL DEFAULT '',
  is_self    INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS people_owner ON people(owner_id);
CREATE UNIQUE INDEX IF NOT EXISTS people_self ON people(owner_id) WHERE is_self = 1;

-- Who was on a trip. These are also the members of the trip's shared
-- expenses (Cospend project members): the id is the member id, weight is the
-- default share, active = still taking part in new bills. updated_ts (unix
-- seconds) lets Cospend clients sync what changed.
CREATE TABLE IF NOT EXISTS trip_people (
  id         INTEGER PRIMARY KEY,
  trip_id    INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
  person_id  INTEGER REFERENCES people(id) ON DELETE SET NULL,
  name       TEXT NOT NULL,
  weight     REAL NOT NULL DEFAULT 1,
  active     INTEGER NOT NULL DEFAULT 1,
  color      TEXT NOT NULL DEFAULT '',
  updated_ts INTEGER NOT NULL DEFAULT (CAST(strftime('%s','now') AS INTEGER)),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS trip_people_trip ON trip_people(trip_id);
CREATE UNIQUE INDEX IF NOT EXISTS trip_people_person ON trip_people(trip_id, person_id) WHERE person_id IS NOT NULL;

-- Everyone gets their own entry ...
INSERT INTO people (owner_id, name, user_id, email, is_self)
  SELECT id, CASE WHEN name <> '' THEN name ELSE email END, id, email, 1 FROM users;
-- ... trip owners are on their trips ...
INSERT INTO trip_people (trip_id, person_id, name)
  SELECT t.id, p.id, p.name FROM trips t JOIN people p ON p.owner_id = t.owner_id AND p.is_self = 1;
-- ... and the companions an owner shared a trip with become people in the
-- owner's book (linked to their account) and are on those trips.
INSERT INTO people (owner_id, name, user_id, email)
  SELECT DISTINCT t.owner_id, CASE WHEN u.name <> '' THEN u.name ELSE u.email END, u.id, u.email
  FROM trip_members m JOIN trips t ON t.id = m.trip_id JOIN users u ON u.id = m.user_id
  WHERE m.user_id <> t.owner_id;
INSERT OR IGNORE INTO trip_people (trip_id, person_id, name)
  SELECT m.trip_id, p.id, p.name FROM trip_members m JOIN trips t ON t.id = m.trip_id
  JOIN people p ON p.owner_id = t.owner_id AND p.user_id = m.user_id AND p.is_self = 0
  WHERE m.user_id <> t.owner_id;
