package providerautomation

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/listmonk/internal/bestedm"
	"github.com/lib/pq"
)

var (
	ErrBusy     = errors.New("rule is executing; retry after it finishes")
	ErrConflict = errors.New("rule has changed; reload before saving or executing")
	ErrPreview  = errors.New("preview these settings before enabling or executing (preview expires after 15 minutes)")
)

type Service struct {
	DB    *sqlx.DB
	Fetch func(context.Context, url.Values) ([]bestedm.Row, error)
	Now   func() time.Time
	Audit func(string, string, Counts)
}

type Counts struct {
	Addresses int `json:"addresses"`
	ToBlock   int `json:"to_block"`
	Already   int `json:"already"`
	Excluded  int `json:"excluded"`
	Unmatched int `json:"unmatched"`
	Skipped   int `json:"skipped"`
}

type Run struct {
	ID         int64      `json:"id" db:"id"`
	Scope      string     `json:"scope" db:"scope"`
	Version    int64      `json:"version" db:"version"`
	Trigger    string     `json:"trigger" db:"trigger"`
	Status     string     `json:"status" db:"status"`
	Start      string     `json:"date_start" db:"date_start"`
	End        string     `json:"date_end" db:"date_end"`
	Counts     Counts     `json:"counts"`
	Error      string     `json:"error" db:"error"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	FinishedAt *time.Time `json:"finished_at" db:"finished_at"`
}

type Item struct {
	TargetID int64    `json:"target_id" db:"target_id"`
	Email    string   `json:"email" db:"email"`
	Name     string   `json:"name" db:"name"`
	Outcome  string   `json:"outcome" db:"outcome"`
	Evidence Evidence `json:"evidence"`
}

type ruleRecord struct {
	Rule
	Raw json.RawMessage `db:"config"`
}

func lockKey(scope string) int64 {
	if scope == "private" {
		return 740701
	}
	return 740702
}

func (s *Service) Rule(ctx context.Context, scope string) (Rule, error) {
	return readRule(ctx, s.DB, scope)
}

func readRule(ctx context.Context, db sqlx.QueryerContext, scope string) (Rule, error) {
	var record ruleRecord
	if err := sqlx.GetContext(ctx, db, &record, `SELECT * FROM provider_blocklist_rules WHERE scope=$1`, scope); err != nil {
		return Rule{}, err
	}
	err := json.Unmarshal(record.Raw, &record.Config)
	return record.Rule, err
}

// Save and Run use the same PostgreSQL advisory lock across processes. Rule
// edits never race an in-flight application of the old configuration.
func (s *Service) Save(ctx context.Context, scope string, version, previewID int64, config Config) (Rule, error) {
	if !ValidScope(scope) {
		return Rule{}, fmt.Errorf("invalid scope")
	}
	if err := config.Validate(); err != nil {
		return Rule{}, err
	}
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return Rule{}, err
	}
	defer tx.Rollback()
	var locked bool
	if err = tx.GetContext(ctx, &locked, `SELECT pg_try_advisory_xact_lock($1)`, lockKey(scope)); err != nil {
		return Rule{}, err
	}
	if !locked {
		return Rule{}, ErrBusy
	}
	rule, err := readRule(ctx, tx, scope)
	if err != nil {
		return Rule{}, err
	}
	if rule.Version != version {
		return Rule{}, ErrConflict
	}
	raw, _ := json.Marshal(config)
	if config.Enabled {
		if err := s.checkPreview(ctx, tx, scope, version, previewID, raw); err != nil {
			return Rule{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE provider_blocklist_rules SET config=$2, version=version+1, next_run_at=$3 WHERE scope=$1`, scope, raw, config.Next(s.Now())); err != nil {
		return Rule{}, err
	}
	if err = tx.Commit(); err != nil {
		return Rule{}, err
	}
	return s.Rule(ctx, scope)
}

func (s *Service) checkPreview(ctx context.Context, db sqlx.QueryerContext, scope string, version, id int64, raw []byte) error {
	var valid bool
	err := sqlx.GetContext(ctx, db, &valid, `SELECT EXISTS(SELECT 1 FROM provider_blocklist_runs
        WHERE id=$1 AND scope=$2 AND version=$3 AND status='preview' AND config=$4::JSONB AND created_at >= $5)`, id, scope, version, raw, s.Now().Add(-15*time.Minute))
	if err != nil {
		return err
	}
	if !valid {
		return ErrPreview
	}
	return nil
}

// Process persists the run before fetching. Target updates, detail records and
// successful completion commit atomically. A crash leaves a visible interrupted
// run, never an unlogged partial application.
func (s *Service) Process(ctx context.Context, scope string, version, previewID int64, draft *Config, scheduled bool) (Run, error) {
	return s.process(ctx, scope, version, previewID, draft, scheduled, false)
}

// Execute applies checked manual conditions without saving or rescheduling the
// recurring rule. A nil config retains the saved-rule API behavior.
func (s *Service) Execute(ctx context.Context, scope string, version, previewID int64, config *Config) (Run, error) {
	return s.process(ctx, scope, version, previewID, config, false, config != nil)
}

func (s *Service) process(ctx context.Context, scope string, version, previewID int64, draft *Config, scheduled, applyDraft bool) (Run, error) {
	if !ValidScope(scope) {
		return Run{}, fmt.Errorf("invalid scope")
	}
	conn, err := s.DB.Connx(ctx)
	if err != nil {
		return Run{}, err
	}
	defer conn.Close()
	var locked bool
	if err = conn.GetContext(ctx, &locked, `SELECT pg_try_advisory_lock($1)`, lockKey(scope)); err != nil {
		return Run{}, err
	}
	if !locked {
		return Run{}, ErrBusy
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, e := conn.ExecContext(cleanup, `SELECT pg_advisory_unlock($1)`, lockKey(scope)); e != nil {
			// Discard the physical connection rather than returning a held lock
			// to the pool after a canceled request or failed unlock.
			_ = conn.Conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	rule, err := readRule(ctx, conn, scope)
	if err != nil {
		return Run{}, err
	}
	// The scope lock proves no live worker still owns an unfinished run.
	if _, err = conn.ExecContext(ctx, `UPDATE provider_blocklist_runs SET status='interrupted', error='worker interrupted; no completed transaction', finished_at=NOW() WHERE scope=$1 AND status='running'`, scope); err != nil {
		return Run{}, err
	}
	now := s.Now()
	if scheduled {
		if !rule.Config.Enabled || rule.NextRunAt == nil || rule.NextRunAt.After(now) {
			return Run{}, nil
		}
	} else if version != rule.Version {
		return Run{}, ErrConflict
	}
	config := rule.Config
	trigger := "manual"
	if scheduled {
		trigger = "scheduled"
	}
	if draft != nil {
		config = *draft
		if !applyDraft {
			trigger = "preview"
		}
	}
	if err = config.Validate(); err != nil {
		return Run{}, err
	}
	raw, _ := json.Marshal(config)
	apply := draft == nil || applyDraft
	if apply && !scheduled {
		if err = s.checkPreview(ctx, conn, scope, version, previewID, raw); err != nil {
			return Run{}, err
		}
	}
	run := Run{Scope: scope, Version: rule.Version, Trigger: trigger, Status: "running", Start: now.AddDate(0, 0, 1-config.LookbackDays).Format("2006-01-02"), End: now.Format("2006-01-02"), CreatedAt: now}
	if err = conn.GetContext(ctx, &run.ID, `INSERT INTO provider_blocklist_runs(scope,version,config,trigger,status,date_start,date_end,created_at)
        VALUES($1,$2,$3,$4,'running',$5,$6,$7) RETURNING id`, scope, rule.Version, raw, trigger, run.Start, run.End, now); err != nil {
		return Run{}, err
	}
	finishError := func(cause error) (Run, error) {
		message := "automation failed; no customer changes were committed"
		var providerError *bestedm.Error
		if errors.As(cause, &providerError) {
			message = providerError.Message + " (" + providerError.Code + ")"
		}
		run.Status, run.Error = "failed", message
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(cleanup, `UPDATE provider_blocklist_runs SET status='failed',error=$2,finished_at=NOW() WHERE id=$1`, run.ID, message)
		if s.Audit != nil {
			s.Audit(scope, "failed", run.Counts)
		}
		return run, cause
	}
	if draft == nil {
		if _, err = conn.ExecContext(ctx, `UPDATE provider_blocklist_rules SET last_run_at=$2,next_run_at=$3 WHERE scope=$1`, scope, now, config.Next(now)); err != nil {
			return finishError(err)
		}
	}
	rows, err := s.Fetch(ctx, url.Values{"date": {run.Start}, "date_end": {run.End}, "type": {"-1"}})
	if err != nil {
		return finishError(err)
	}
	evidence := Select(rows, config)
	tx, err := conn.BeginTxx(ctx, nil)
	if err != nil {
		return finishError(err)
	}
	defer tx.Rollback()
	items, counts, err := match(ctx, tx, scope, config, evidence, apply)
	if err != nil {
		tx.Rollback()
		return finishError(err)
	}
	run.Counts = counts
	itemJSON, _ := json.Marshal(items)
	if _, err = tx.ExecContext(ctx, `INSERT INTO provider_blocklist_items(run_id,position,target_id,email,name,evidence,outcome)
        SELECT $1,position-1,(item->>'target_id')::BIGINT,item->>'email',item->>'name',item->'evidence',item->>'outcome'
        FROM jsonb_array_elements($2::JSONB) WITH ORDINALITY AS entries(item,position)`, run.ID, itemJSON); err != nil {
		tx.Rollback()
		return finishError(err)
	}
	run.Status = "success"
	if !apply {
		run.Status = "preview"
	}
	countJSON, _ := json.Marshal(counts)
	if _, err = tx.ExecContext(ctx, `UPDATE provider_blocklist_runs SET status=$2,counts=$3,finished_at=NOW() WHERE id=$1`, run.ID, run.Status, countJSON); err != nil {
		tx.Rollback()
		return finishError(err)
	}
	if err = tx.Commit(); err != nil {
		return finishError(err)
	}
	if s.Audit != nil && apply {
		s.Audit(scope, "success", counts)
	}
	return run, nil
}

func match(ctx context.Context, tx *sqlx.Tx, scope string, config Config, evidence []Evidence, apply bool) ([]Item, Counts, error) {
	items := []Item{}
	counts := Counts{Addresses: len(evidence)}
	if len(evidence) == 0 {
		return items, counts, nil
	}
	addresses := make([]string, 0, len(evidence))
	for _, e := range evidence {
		addresses = append(addresses, e.Email)
	}
	table := "customers"
	predicate := `transfer_pending_at IS NULL AND (organization_id IS NULL OR EXISTS (SELECT 1 FROM organizations o WHERE o.id=customers.organization_id AND o.archived_at IS NULL))`
	if scope == "pool" {
		table = "pool_contacts"
		predicate = `status <> 'archived' AND EXISTS (SELECT 1 FROM pool_members pm WHERE pm.contact_id=pool_contacts.id)`
	}
	var targets []struct {
		ID       int64  `db:"id"`
		Email    string `db:"email"`
		Name     string `db:"name"`
		Status   string `db:"status"`
		Eligible bool   `db:"eligible"`
	}
	query := `SELECT id,LOWER(TRIM(email)) AS email,COALESCE(name,'') AS name,status,(` + predicate + `) AS eligible FROM ` + table + ` WHERE LOWER(TRIM(email))=ANY($1::TEXT[]) ORDER BY id`
	if apply {
		query += " FOR UPDATE"
	}
	if err := tx.SelectContext(ctx, &targets, query, pq.Array(addresses)); err != nil {
		return nil, counts, err
	}
	byEmail := map[string][]int{}
	for i, t := range targets {
		byEmail[t.Email] = append(byEmail[t.Email], i)
	}
	excluded := map[string]bool{}
	for _, email := range config.ExcludedEmails {
		excluded[email] = true
	}
	ids := []int64{}
	for _, e := range evidence {
		matches := byEmail[e.Email]
		if excluded[e.Email] {
			counts.Excluded++
			items = append(items, Item{Email: e.Email, Evidence: e, Outcome: "excluded"})
			continue
		}
		if len(matches) == 0 {
			counts.Unmatched++
			items = append(items, Item{Email: e.Email, Evidence: e, Outcome: "unmatched"})
		}
		for _, index := range matches {
			t := targets[index]
			outcome := "to_block"
			switch {
			case !t.Eligible:
				outcome = "skipped"
				counts.Skipped++
			case t.Status == "blocklisted":
				outcome = "already"
				counts.Already++
			default:
				counts.ToBlock++
				ids = append(ids, t.ID)
				if apply {
					outcome = "blocked"
				}
			}
			items = append(items, Item{TargetID: t.ID, Email: e.Email, Name: t.Name, Evidence: e, Outcome: outcome})
		}
	}
	if apply && len(ids) > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET status='blocklisted',updated_at=NOW() WHERE id=ANY($1::BIGINT[])`, pq.Array(ids)); err != nil {
			return nil, counts, err
		}
		if scope == "private" {
			if _, err := tx.ExecContext(ctx, `UPDATE customer_list_memberships SET status='unsubscribed',updated_at=NOW() WHERE customer_id=ANY($1::BIGINT[])`, pq.Array(ids)); err != nil {
				return nil, counts, err
			}
		}
	}
	return items, counts, nil
}

func (s *Service) Runs(ctx context.Context, scope string) ([]Run, error) {
	var records []struct {
		Run
		Raw json.RawMessage `db:"counts"`
	}
	err := s.DB.SelectContext(ctx, &records, `SELECT id,scope,version,trigger,status,date_start,date_end,counts,error,created_at,finished_at FROM provider_blocklist_runs WHERE scope=$1 ORDER BY id DESC LIMIT 20`, scope)
	out := []Run{}
	for _, r := range records {
		if e := json.Unmarshal(r.Raw, &r.Counts); e != nil {
			return nil, e
		}
		out = append(out, r.Run)
	}
	return out, err
}

func (s *Service) Items(ctx context.Context, scope string, runID int64, page int) ([]Item, error) {
	var records []struct {
		Item
		Raw json.RawMessage `db:"evidence"`
	}
	err := s.DB.SelectContext(ctx, &records, `SELECT i.target_id,i.email,i.name,i.evidence,i.outcome FROM provider_blocklist_items i JOIN provider_blocklist_runs r ON r.id=i.run_id WHERE r.scope=$1 AND r.id=$2 ORDER BY i.position LIMIT 50 OFFSET $3`, scope, runID, (page-1)*50)
	out := []Item{}
	for _, r := range records {
		if e := json.Unmarshal(r.Raw, &r.Evidence); e != nil {
			return nil, e
		}
		out = append(out, r.Item)
	}
	return out, err
}

// Avoid leaking database statements and email addresses through error text.
func SafeError(err error) string {
	if errors.Is(err, ErrBusy) || errors.Is(err, ErrConflict) || errors.Is(err, ErrPreview) {
		return err.Error()
	}
	var pe *bestedm.Error
	if errors.As(err, &pe) {
		return pe.Message + " (" + pe.Code + ")"
	}
	return "automation failed; reload and retry"
}
