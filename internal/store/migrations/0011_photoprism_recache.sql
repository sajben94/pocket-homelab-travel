-- PhotoPrism searches stopped after the first batch when photos had several
-- files (RAW + JPEG): drop what was kept so the trips load in full.
DELETE FROM cache WHERE key LIKE '%:photoprism-photos' OR key LIKE 'user:%:photo-trips';
