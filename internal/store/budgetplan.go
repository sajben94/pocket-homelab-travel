package store

import "context"

// PlanLine is one category of a trip's budget plan.
type PlanLine struct {
	Category string `json:"category"`
	Amount   int64  `json:"amount"`  // minor units of the trip currency
	PerDay   bool   `json:"per_day"` // Amount is for each day of the trip
}

// Total is what the line allows over the whole trip.
func (l PlanLine) Total(days int) int64 {
	if l.PerDay {
		if days < 1 {
			days = 1
		}
		return l.Amount * int64(days)
	}
	return l.Amount
}

// BudgetPlan returns a trip's plan, in the order it was saved.
func (s *Store) BudgetPlan(ctx context.Context, tripID int64) ([]PlanLine, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT category, amount, per_day FROM budget_plan WHERE trip_id = ? ORDER BY rowid`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlanLine{}
	for rows.Next() {
		var l PlanLine
		if err := rows.Scan(&l.Category, &l.Amount, &l.PerDay); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SetBudgetPlan replaces a trip's plan; with any lines, the trip budget
// becomes their total.
func (s *Store) SetBudgetPlan(ctx context.Context, tripID int64, days int, lines []PlanLine) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM budget_plan WHERE trip_id = ?`, tripID); err != nil {
		return err
	}
	var total int64
	for _, l := range lines {
		if l.Category == "" || l.Amount <= 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO budget_plan (trip_id, category, amount, per_day) VALUES (?, ?, ?, ?)
			ON CONFLICT(trip_id, category) DO UPDATE SET amount = excluded.amount, per_day = excluded.per_day`, tripID, l.Category, l.Amount, l.PerDay); err != nil {
			return err
		}
		total += l.Total(days)
	}
	if total > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE trips SET budget = ?, updated_at = ? WHERE id = ?`, total, Now(), tripID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
