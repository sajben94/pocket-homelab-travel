-- Which country each located thing is in (ISO 3166-1 alpha-2, upper case),
-- looked up from its coordinates in the background; '' = not known yet.
-- trips.countries is the person's own list for the trip (comma separated);
-- '' = use what was detected from the trip's stays and places.
ALTER TABLE places  ADD COLUMN country_code TEXT NOT NULL DEFAULT '';
ALTER TABLE lodging ADD COLUMN country_code TEXT NOT NULL DEFAULT '';
ALTER TABLE trips   ADD COLUMN country_code TEXT NOT NULL DEFAULT '';
ALTER TABLE trips   ADD COLUMN countries    TEXT NOT NULL DEFAULT '';
