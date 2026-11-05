package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

const expenseCols = `id, trip_id, name, category, amount, currency, amount_home, fx_rate, date, kind, source, external_id, link_kind, link_id, notes, created_at, updated_at, payer_id, payment_mode_id, bill_time, deleted, updated_ts, repeat, repeat_all_active, repeat_until, repeat_freq, parent_id`

func scanExpense(row interface{ Scan(...any) error }) (*Expense, error) {
	var e Expense
	var payer, parent sql.NullInt64
	var del, all int
	if err := row.Scan(&e.ID, &e.TripID, &e.Name, &e.Category, &e.Amount, &e.Currency, &e.AmountHome, &e.FXRate, &e.Date, &e.Kind, &e.Source, &e.ExternalID, &e.LinkKind, &e.LinkID, &e.Notes, &e.CreatedAt, &e.UpdatedAt,
		&payer, &e.PaymentModeID, &e.BillTime, &del, &e.UpdatedTS, &e.Repeat, &all, &e.RepeatUntil, &e.RepeatFreq, &parent); err != nil {
		return nil, notFound(err)
	}
	if payer.Valid {
		e.PayerID = &payer.Int64
	}
	if parent.Valid {
		e.ParentID = &parent.Int64
	}
	e.Deleted, e.RepeatAllActive = del == 1, all == 1
	e.Owers = []int64{}
	return &e, nil
}

// ExpenseFilter narrows ListExpensesWith.
type ExpenseFilter struct {
	Deleted     *bool // nil = not in the trash; true = only the trash
	BillsOnly   bool  // spent costs only (Cospend bills)
	ChangedFrom int64 // updated_ts >= this (Cospend "lastchanged")
}

// ListExpenses returns a trip's expenses outside the trash, newest date first.
func (s *Store) ListExpenses(ctx context.Context, tripID int64) ([]Expense, error) {
	return s.ListExpensesWith(ctx, tripID, ExpenseFilter{})
}

// ListExpensesWith returns a trip's expenses with their owers.
func (s *Store) ListExpensesWith(ctx context.Context, tripID int64, f ExpenseFilter) ([]Expense, error) {
	q := `SELECT ` + expenseCols + ` FROM expenses WHERE trip_id = ?`
	args := []any{tripID}
	if f.Deleted != nil && *f.Deleted {
		q += ` AND deleted = 1`
	} else if f.Deleted == nil {
		q += ` AND deleted = 0`
	}
	if f.BillsOnly {
		q += ` AND kind = 'actual'`
	}
	if f.ChangedFrom > 0 {
		q += ` AND updated_ts >= ?`
		args = append(args, f.ChangedFrom)
	}
	q += ` ORDER BY date DESC, bill_time DESC, id DESC`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out := []Expense{}
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	owers, err := s.tripOwers(ctx, tripID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if o := owers[out[i].ID]; o != nil {
			out[i].Owers = o
		}
	}
	return out, nil
}

func (s *Store) tripOwers(ctx context.Context, tripID int64) (map[int64][]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT o.expense_id, o.member_id FROM expense_owers o JOIN expenses e ON e.id = o.expense_id WHERE e.trip_id = ? ORDER BY o.member_id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var e, m int64
		if err := rows.Scan(&e, &m); err != nil {
			return nil, err
		}
		out[e] = append(out[e], m)
	}
	return out, rows.Err()
}

// BillIDs returns the ids of a trip's bills outside the trash (Cospend "allBillIds").
func (s *Store) BillIDs(ctx context.Context, tripID int64) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM expenses WHERE trip_id = ? AND kind = 'actual' AND deleted = 0 ORDER BY id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GetExpense loads one expense (also from the trash) with its owers.
func (s *Store) GetExpense(ctx context.Context, tripID, id int64) (*Expense, error) {
	e, err := scanExpense(s.DB.QueryRowContext(ctx, `SELECT `+expenseCols+` FROM expenses WHERE id = ? AND trip_id = ?`, id, tripID))
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT member_id FROM expense_owers WHERE expense_id = ? ORDER BY member_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m int64
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		e.Owers = append(e.Owers, m)
	}
	return e, rows.Err()
}

func defaults(e *Expense) {
	if e.Kind == "" {
		e.Kind = "planned"
	}
	if e.Source == "" {
		e.Source = "manual"
	}
	if e.Category == "" {
		e.Category = "other"
	}
	if e.FXRate == 0 {
		e.FXRate = 1
	}
	if e.Repeat == "" {
		e.Repeat = "n"
	}
	if e.RepeatFreq <= 0 {
		e.RepeatFreq = 1
	}
	e.UpdatedTS = time.Now().Unix()
}

func writeOwers(ctx context.Context, tx *sql.Tx, id int64, owers []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM expense_owers WHERE expense_id = ?`, id); err != nil {
		return err
	}
	for _, m := range owers {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO expense_owers (expense_id, member_id) VALUES (?, ?)`, id, m); err != nil {
			return err
		}
	}
	return nil
}

// CreateExpense inserts an expense (and, for a bill, who it is for).
func (s *Store) CreateExpense(ctx context.Context, e *Expense, after ...core.CommitHook) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	defaults(e)
	res, err := tx.ExecContext(ctx, `INSERT INTO expenses (trip_id, name, category, amount, currency, amount_home, fx_rate, date, kind, source, external_id, link_kind, link_id, notes,
		payer_id, payment_mode_id, bill_time, deleted, updated_ts, repeat, repeat_all_active, repeat_until, repeat_freq, parent_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.TripID, e.Name, e.Category, e.Amount, e.Currency, e.AmountHome, e.FXRate, e.Date, e.Kind, e.Source, e.ExternalID, e.LinkKind, e.LinkID, e.Notes,
		e.PayerID, e.PaymentModeID, e.BillTime, boolInt(e.Deleted), e.UpdatedTS, e.Repeat, boolInt(e.RepeatAllActive), e.RepeatUntil, e.RepeatFreq, e.ParentID)
	if err != nil {
		return conflict(err)
	}
	e.ID, _ = res.LastInsertId()
	if e.Owers == nil {
		e.Owers = []int64{}
	}
	if err := writeOwers(ctx, tx, e.ID, e.Owers); err != nil {
		return err
	}
	return core.Commit(tx, after...)
}

// UpdateExpense saves an expense; owers are rewritten when e.Owers is not nil.
func (s *Store) UpdateExpense(ctx context.Context, e *Expense) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	defaults(e)
	if _, err := tx.ExecContext(ctx, `UPDATE expenses SET name = ?, category = ?, amount = ?, currency = ?, amount_home = ?, fx_rate = ?, date = ?, kind = ?, link_kind = ?, link_id = ?, notes = ?, updated_at = ?,
		payer_id = ?, payment_mode_id = ?, bill_time = ?, deleted = ?, updated_ts = ?, repeat = ?, repeat_all_active = ?, repeat_until = ?, repeat_freq = ? WHERE id = ? AND trip_id = ?`,
		e.Name, e.Category, e.Amount, e.Currency, e.AmountHome, e.FXRate, e.Date, e.Kind, e.LinkKind, e.LinkID, e.Notes, Now(),
		e.PayerID, e.PaymentModeID, e.BillTime, boolInt(e.Deleted), e.UpdatedTS, e.Repeat, boolInt(e.RepeatAllActive), e.RepeatUntil, e.RepeatFreq, e.ID, e.TripID); err != nil {
		return err
	}
	if e.Owers != nil {
		if err := writeOwers(ctx, tx, e.ID, e.Owers); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TrashExpense moves a bill to the trash (or back out of it).
func (s *Store) TrashExpense(ctx context.Context, tripID, id int64, deleted bool) error {
	// the extra parts of an uneven split go (and come back) with their bill
	res, err := s.DB.ExecContext(ctx, `UPDATE expenses SET deleted = ?, updated_ts = ?, updated_at = ? WHERE (id = ? OR parent_id = ?) AND trip_id = ?`, boolInt(deleted), time.Now().Unix(), Now(), id, id, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// EmptyTrash deletes the bills in a trip's trash for good.
func (s *Store) EmptyTrash(ctx context.Context, tripID int64) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM expenses WHERE trip_id = ? AND deleted = 1`, tripID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MemberBills counts the bills (trash included) someone on a trip paid or owes.
func (s *Store) MemberBills(ctx context.Context, tripID, memberID int64) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM expenses e WHERE e.trip_id = ? AND (e.payer_id = ? OR EXISTS (SELECT 1 FROM expense_owers o WHERE o.expense_id = e.id AND o.member_id = ?))`, tripID, memberID, memberID).Scan(&n)
	return n, err
}

// RepeatingBills returns every bill outside the trash that repeats.
func (s *Store) RepeatingBills(ctx context.Context) ([]Expense, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+expenseCols+` FROM expenses WHERE kind = 'actual' AND deleted = 0 AND repeat <> 'n' AND repeat <> '' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []Expense
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *e)
	}
	rows.Close()
	for i := range out {
		if full, err := s.GetExpense(ctx, out[i].TripID, out[i].ID); err == nil {
			out[i].Owers = full.Owers
		}
	}
	return out, rows.Err()
}

// BillParts returns the extra parts of an unevenly split bill.
func (s *Store) BillParts(ctx context.Context, tripID, parentID int64) ([]Expense, error) {
	all, err := s.ListExpensesWith(ctx, tripID, ExpenseFilter{Deleted: new(bool)})
	if err != nil {
		return nil, err
	}
	var out []Expense
	for _, e := range all {
		if e.ParentID != nil && *e.ParentID == parentID {
			out = append(out, e)
		}
	}
	return out, nil
}

// MoveMemberBills moves every bill of one trip member to another (merging people).
func MoveMemberBills(ctx context.Context, tx *sql.Tx, tripID, from, into int64) error {
	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `UPDATE expenses SET payer_id = ?, updated_ts = ? WHERE trip_id = ? AND payer_id = ?`, into, now, tripID, from); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE expenses SET updated_ts = ? WHERE trip_id = ? AND id IN (SELECT expense_id FROM expense_owers WHERE member_id = ?)`, now, tripID, from); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO expense_owers (expense_id, member_id) SELECT expense_id, ? FROM expense_owers WHERE member_id = ?`, into, from); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM expense_owers WHERE member_id = ?`, from)
	return err
}

// SyncedSources are the expense sources written by the trip-cost sync.
var SyncedSources = []string{"money", "firefly"}

// IsSynced reports whether an expense came from the trip-cost sync (its
// amount and date belong to Pocket Money / Firefly, not to this app).
func IsSynced(source string) bool { return source == "money" || source == "firefly" }

// RekeyExternalExpense moves a synced row to another source/external id, so
// a cost first synced from Firefly keeps its row (and the category someone
// picked for it) when the same transaction now comes from Pocket Money.
func (s *Store) RekeyExternalExpense(ctx context.Context, tripID, id int64, source, externalID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE expenses SET source = ?, external_id = ?, updated_at = ? WHERE id = ? AND trip_id = ?`, source, externalID, Now(), id, tripID)
	return conflict(err)
}

// DeleteSyncedExpensesExcept removes the trip's synced rows (any of sources)
// whose id is not in keep: what the tag no longer carries upstream.
func (s *Store) DeleteSyncedExpensesExcept(ctx context.Context, tripID int64, sources []string, keep []int64) (int64, error) {
	if len(sources) == 0 {
		return 0, nil
	}
	q := `DELETE FROM expenses WHERE trip_id = ? AND source IN (?` + strings.Repeat(",?", len(sources)-1) + `)`
	args := []any{tripID}
	for _, src := range sources {
		args = append(args, src)
	}
	if len(keep) > 0 {
		q += ` AND id NOT IN (?` + strings.Repeat(",?", len(keep)-1) + `)`
		for _, id := range keep {
			args = append(args, id)
		}
	}
	res, err := s.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpsertExternalExpense inserts or updates a synced row (Pocket Money / Firefly) by source + external id.
// Returns true when a new row was created.
func (s *Store) UpsertExternalExpense(ctx context.Context, e *Expense, after ...core.CommitHook) (bool, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM expenses WHERE trip_id = ? AND source = ? AND external_id = ?`, e.TripID, e.Source, e.ExternalID).Scan(&id)
	if err == nil {
		e.ID = id
		_, err = s.DB.ExecContext(ctx, `UPDATE expenses SET name = ?, category = ?, amount = ?, currency = ?, amount_home = ?, fx_rate = ?, date = ?, kind = ?, notes = ?, updated_at = ?,
			updated_ts = CASE WHEN name = ? AND amount = ? AND currency = ? AND date = ? AND kind = ? AND notes = ? THEN updated_ts ELSE ? END WHERE id = ?`,
			e.Name, e.Category, e.Amount, e.Currency, e.AmountHome, e.FXRate, e.Date, e.Kind, e.Notes, Now(),
			e.Name, e.Amount, e.Currency, e.Date, e.Kind, e.Notes, time.Now().Unix(), id)
		return false, err
	}
	return true, s.CreateExpense(ctx, e, after...)
}

// DeleteExpense removes an expense.
func (s *Store) DeleteExpense(ctx context.Context, tripID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM expenses WHERE id = ? AND trip_id = ?`, id, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteExternalExpensesNotIn removes synced rows whose external id is no longer present upstream.
func (s *Store) DeleteExternalExpensesNotIn(ctx context.Context, tripID int64, source string, keep []string) (int64, error) {
	if len(keep) == 0 {
		res, err := s.DB.ExecContext(ctx, `DELETE FROM expenses WHERE trip_id = ? AND source = ?`, tripID, source)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	q := `DELETE FROM expenses WHERE trip_id = ? AND source = ? AND external_id NOT IN (`
	args := []any{tripID, source}
	for i, k := range keep {
		if i > 0 {
			q += ","
		}
		q += "?"
		args = append(args, k)
	}
	q += ")"
	res, err := s.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---- checklists ----

// ListChecklists returns a trip's checklists with items.
func (s *Store) ListChecklists(ctx context.Context, tripID int64) ([]Checklist, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, trip_id, name, kind, sort_order, created_at FROM checklists WHERE trip_id = ? ORDER BY sort_order, id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Checklist{}
	idx := map[int64]int{}
	for rows.Next() {
		var c Checklist
		if err := rows.Scan(&c.ID, &c.TripID, &c.Name, &c.Kind, &c.SortOrder, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Items = []ChecklistItem{}
		idx[c.ID] = len(out)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	irows, err := s.DB.QueryContext(ctx, `SELECT i.id, i.checklist_id, i.text, i.done, i.assignee_id, i.sort_order, i.created_at FROM checklist_items i JOIN checklists c ON c.id = i.checklist_id WHERE c.trip_id = ? ORDER BY i.sort_order, i.id`, tripID)
	if err != nil {
		return nil, err
	}
	defer irows.Close()
	for irows.Next() {
		var it ChecklistItem
		var done int
		if err := irows.Scan(&it.ID, &it.ChecklistID, &it.Text, &done, &it.AssigneeID, &it.SortOrder, &it.CreatedAt); err != nil {
			return nil, err
		}
		it.Done = done == 1
		if i, ok := idx[it.ChecklistID]; ok {
			out[i].Items = append(out[i].Items, it)
		}
	}
	return out, irows.Err()
}

// CreateChecklist inserts a list.
func (s *Store) CreateChecklist(ctx context.Context, c *Checklist) error {
	if c.Kind == "" {
		c.Kind = "todo"
	}
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 1 FROM checklists WHERE trip_id = ?`, c.TripID).Scan(&c.SortOrder)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO checklists (trip_id, name, kind, sort_order) VALUES (?, ?, ?, ?)`, c.TripID, c.Name, c.Kind, c.SortOrder)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	c.Items = []ChecklistItem{}
	return nil
}

// UpdateChecklist renames a list.
func (s *Store) UpdateChecklist(ctx context.Context, tripID, id int64, name, kind string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE checklists SET name = ?, kind = ? WHERE id = ? AND trip_id = ?`, name, kind, id, tripID)
	return err
}

// DeleteChecklist removes a list and its items.
func (s *Store) DeleteChecklist(ctx context.Context, tripID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM checklists WHERE id = ? AND trip_id = ?`, id, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// checklistOwned checks the list belongs to the trip.
func (s *Store) checklistOwned(ctx context.Context, tripID, listID int64) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM checklists WHERE id = ? AND trip_id = ?`, listID, tripID).Scan(&n)
	return n > 0
}

// AddChecklistItem appends an item.
func (s *Store) AddChecklistItem(ctx context.Context, tripID int64, it *ChecklistItem) error {
	if !s.checklistOwned(ctx, tripID, it.ChecklistID) {
		return ErrNotFound
	}
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order), 0) + 1 FROM checklist_items WHERE checklist_id = ?`, it.ChecklistID).Scan(&it.SortOrder)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO checklist_items (checklist_id, text, done, assignee_id, sort_order) VALUES (?, ?, ?, ?, ?)`, it.ChecklistID, it.Text, boolInt(it.Done), it.AssigneeID, it.SortOrder)
	if err != nil {
		return err
	}
	it.ID, _ = res.LastInsertId()
	return nil
}

// UpdateChecklistItem saves text/done/assignee.
func (s *Store) UpdateChecklistItem(ctx context.Context, tripID int64, it *ChecklistItem) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE checklist_items SET text = ?, done = ?, assignee_id = ?, sort_order = ? WHERE id = ? AND checklist_id IN (SELECT id FROM checklists WHERE trip_id = ?)`, it.Text, boolInt(it.Done), it.AssigneeID, it.SortOrder, it.ID, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetChecklistItem loads one item.
func (s *Store) GetChecklistItem(ctx context.Context, tripID, id int64) (*ChecklistItem, error) {
	var it ChecklistItem
	var done int
	err := s.DB.QueryRowContext(ctx, `SELECT i.id, i.checklist_id, i.text, i.done, i.assignee_id, i.sort_order, i.created_at FROM checklist_items i JOIN checklists c ON c.id = i.checklist_id WHERE i.id = ? AND c.trip_id = ?`, id, tripID).Scan(&it.ID, &it.ChecklistID, &it.Text, &done, &it.AssigneeID, &it.SortOrder, &it.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	it.Done = done == 1
	return &it, nil
}

// DeleteChecklistItem removes an item.
func (s *Store) DeleteChecklistItem(ctx context.Context, tripID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM checklist_items WHERE id = ? AND checklist_id IN (SELECT id FROM checklists WHERE trip_id = ?)`, id, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- journal ----

const journalCols = `j.id, j.trip_id, j.user_id, COALESCE(u.name, ''), j.date, j.title, j.body, j.mood, j.lat, j.lng, j.created_at, j.updated_at`

func scanJournal(row interface{ Scan(...any) error }) (*JournalEntry, error) {
	var j JournalEntry
	if err := row.Scan(&j.ID, &j.TripID, &j.UserID, &j.UserName, &j.Date, &j.Title, &j.Body, &j.Mood, &j.Lat, &j.Lng, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	return &j, nil
}

// ListJournal returns entries by date.
func (s *Store) ListJournal(ctx context.Context, tripID int64) ([]JournalEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+journalCols+` FROM journal j LEFT JOIN users u ON u.id = j.user_id WHERE j.trip_id = ? ORDER BY j.date, j.id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JournalEntry{}
	for rows.Next() {
		j, err := scanJournal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *j)
	}
	return out, rows.Err()
}

// LatestJournal returns the newest entries across the trips a user can see.
func (s *Store) LatestJournal(ctx context.Context, userID int64, limit int) ([]JournalEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+journalCols+`, t.name FROM journal j JOIN trips t ON t.id = j.trip_id LEFT JOIN users u ON u.id = j.user_id
		WHERE t.archived = 0 AND (t.owner_id = ? OR t.id IN (SELECT trip_id FROM trip_members WHERE user_id = ?)) ORDER BY j.date DESC, j.id DESC LIMIT ?`, userID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JournalEntry{}
	for rows.Next() {
		var j JournalEntry
		if err := rows.Scan(&j.ID, &j.TripID, &j.UserID, &j.UserName, &j.Date, &j.Title, &j.Body, &j.Mood, &j.Lat, &j.Lng, &j.CreatedAt, &j.UpdatedAt, &j.TripName); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// GetJournal loads one entry.
func (s *Store) GetJournal(ctx context.Context, tripID, id int64) (*JournalEntry, error) {
	return scanJournal(s.DB.QueryRowContext(ctx, `SELECT `+journalCols+` FROM journal j LEFT JOIN users u ON u.id = j.user_id WHERE j.id = ? AND j.trip_id = ?`, id, tripID))
}

// CreateJournal inserts an entry.
func (s *Store) CreateJournal(ctx context.Context, j *JournalEntry) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO journal (trip_id, user_id, date, title, body, mood, lat, lng) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, j.TripID, j.UserID, j.Date, j.Title, j.Body, j.Mood, j.Lat, j.Lng)
	if err != nil {
		return err
	}
	j.ID, _ = res.LastInsertId()
	return nil
}

// UpdateJournal saves an entry.
func (s *Store) UpdateJournal(ctx context.Context, j *JournalEntry) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE journal SET date = ?, title = ?, body = ?, mood = ?, lat = ?, lng = ?, updated_at = ? WHERE id = ? AND trip_id = ?`, j.Date, j.Title, j.Body, j.Mood, j.Lat, j.Lng, Now(), j.ID, j.TripID)
	return err
}

// DeleteJournal removes an entry.
func (s *Store) DeleteJournal(ctx context.Context, tripID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM journal WHERE id = ? AND trip_id = ?`, id, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- documents ----

const docCols = `id, trip_id, user_id, name, filename, mime, bytes, kind, link_kind, link_id, created_at`

func scanDoc(row interface{ Scan(...any) error }) (*Document, error) {
	var d Document
	if err := row.Scan(&d.ID, &d.TripID, &d.UserID, &d.Name, &d.Filename, &d.Mime, &d.Bytes, &d.Kind, &d.LinkKind, &d.LinkID, &d.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	return &d, nil
}

// ListDocuments returns a trip's files.
func (s *Store) ListDocuments(ctx context.Context, tripID int64) ([]Document, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+docCols+` FROM documents WHERE trip_id = ? ORDER BY kind, name`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Document{}
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// GetDocument loads one file record.
func (s *Store) GetDocument(ctx context.Context, tripID, id int64) (*Document, error) {
	return scanDoc(s.DB.QueryRowContext(ctx, `SELECT `+docCols+` FROM documents WHERE id = ? AND trip_id = ?`, id, tripID))
}

// CreateDocument inserts a record.
func (s *Store) CreateDocument(ctx context.Context, d *Document) error {
	if d.Kind == "" {
		d.Kind = "other"
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO documents (trip_id, user_id, name, filename, mime, bytes, kind, link_kind, link_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, d.TripID, d.UserID, d.Name, d.Filename, d.Mime, d.Bytes, d.Kind, d.LinkKind, d.LinkID)
	if err != nil {
		return err
	}
	d.ID, _ = res.LastInsertId()
	return nil
}

// UpdateDocument renames / re-links.
func (s *Store) UpdateDocument(ctx context.Context, d *Document) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE documents SET name = ?, kind = ?, link_kind = ?, link_id = ? WHERE id = ? AND trip_id = ?`, d.Name, d.Kind, d.LinkKind, d.LinkID, d.ID, d.TripID)
	return err
}

// DeleteDocument removes the record (the caller removes the file).
func (s *Store) DeleteDocument(ctx context.Context, tripID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM documents WHERE id = ? AND trip_id = ?`, id, tripID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TripFilenames lists every stored file of a trip (for deletion).
func (s *Store) TripFilenames(ctx context.Context, tripID int64) []string {
	var out []string
	rows, err := s.DB.QueryContext(ctx, `SELECT filename FROM documents WHERE trip_id = ? UNION SELECT image_path FROM places WHERE trip_id = ? AND image_path <> '' UNION SELECT image_path FROM lodging WHERE trip_id = ? AND image_path <> '' UNION SELECT cover_path FROM trips WHERE id = ? AND cover_path <> ''`, tripID, tripID, tripID, tripID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var f string
		if rows.Scan(&f) == nil && f != "" {
			out = append(out, f)
		}
	}
	return out
}
