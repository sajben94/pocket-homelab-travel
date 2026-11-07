-- Where someone said a trip found in their photos is really two: a new trip
-- starts on date.
CREATE TABLE photo_trip_splits (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    date    TEXT    NOT NULL,
    PRIMARY KEY (user_id, date)
);
-- found trips now keep a summary per day (needed to split them): find them again
DELETE FROM cache WHERE key LIKE 'user:%:photo-trips';
