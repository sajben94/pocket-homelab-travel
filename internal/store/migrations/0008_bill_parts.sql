-- An uneven split is stored as Cospend does it: the main bill carries what is
-- shared equally (by weight) among everyone it is for, and each extra part
-- someone owes on top is its own small bill (parent_id = the main bill),
-- paid by the same payer, for that one person. Cospend clients see plain
-- bills; this app shows one bill with extras.
ALTER TABLE expenses ADD COLUMN parent_id INTEGER REFERENCES expenses(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS expenses_parent ON expenses(parent_id) WHERE parent_id IS NOT NULL;
