-- Photos now say which phone or app they came from: fetch them again.
DELETE FROM cache WHERE key LIKE '%:photoprism-photos' OR key LIKE '%:immich-photos' OR key LIKE 'user:%:photo-trips';
