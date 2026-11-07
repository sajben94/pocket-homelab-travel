-- Photos someone hid from a trip's Photos tab (a picture taken at home on the
-- first morning, say). Per viewer: everyone sees photos from their own
-- library. photo is "immich:<asset id>" or "photoprism:<uid>".
CREATE TABLE trip_photo_hidden (
    trip_id INTEGER NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    photo   TEXT    NOT NULL,
    PRIMARY KEY (trip_id, user_id, photo)
);

-- Trips found in someone's photos that they said are not trips.
CREATE TABLE photo_trip_dismissed (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    start_date TEXT NOT NULL,
    PRIMARY KEY (user_id, start_date)
);
