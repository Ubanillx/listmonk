package dataexport

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/jmoiron/sqlx"
	"github.com/xuri/excelize/v2"
)

const jobColumns = `id,user_id,organization_id,request,status,filename,row_count,error,created_at,started_at,completed_at,expires_at,download_count,last_downloaded_at`

type Job struct {
	ID               string          `db:"id" json:"id"`
	UserID           int             `db:"user_id" json:"user_id"`
	OrganizationID   int             `db:"organization_id" json:"organization_id"`
	Request          json.RawMessage `db:"request" json:"request"`
	Status           string          `db:"status" json:"status"`
	Filename         string          `db:"filename" json:"filename"`
	RowCount         int64           `db:"row_count" json:"row_count"`
	Error            string          `db:"error" json:"error"`
	CreatedAt        time.Time       `db:"created_at" json:"created_at"`
	StartedAt        *time.Time      `db:"started_at" json:"started_at"`
	CompletedAt      *time.Time      `db:"completed_at" json:"completed_at"`
	ExpiresAt        time.Time       `db:"expires_at" json:"expires_at"`
	DownloadCount    int             `db:"download_count" json:"download_count"`
	LastDownloadedAt *time.Time      `db:"last_downloaded_at" json:"last_downloaded_at"`
}

type Service struct {
	DB  *sqlx.DB
	Log *log.Logger
}

// Access is reloaded from the database, not from the saved job or a browser
// role flag. The stamp invalidates artifacts after privacy/role/list changes.
func (s *Service) Access(ctx context.Context, userID, orgID int) (Access, string, error) {
	a := Access{UserID: userID, OrganizationID: orgID}
	var admin, allowed bool
	err := s.DB.QueryRowContext(ctx, `SELECT u.user_role_id=1,
 u.status='enabled' AND ((u.user_role_id=1 AND ($2=0 OR EXISTS(SELECT 1 FROM organizations o WHERE o.id=$2 AND o.status='active')))
 OR ($2>0 AND EXISTS(SELECT 1 FROM organization_members m JOIN organizations o ON o.id=m.organization_id WHERE m.user_id=u.id AND m.organization_id=$2 AND m.role='manager' AND m.removed_at IS NULL AND o.status='active')))
 FROM users u WHERE u.id=$1`, userID, orgID).Scan(&admin, &allowed)
	if err != nil {
		return a, "", err
	}
	if !allowed {
		return a, "", fmt.Errorf("仅最高管理员或当前组织管理员可导出")
	}
	a.PlatformAdmin = admin
	var stamp string
	err = s.DB.QueryRowContext(ctx, `SELECT
 COALESCE((SELECT value::text::boolean FROM settings WHERE key='privacy.individual_tracking'),false),
 COALESCE((SELECT value::text::boolean FROM settings WHERE key='privacy.disable_tracking'),false),
 md5(COALESCE((SELECT string_agg(id::text || ':' || mask_emails::text || ':' || COALESCE(owner_user_id,0)::text,',' ORDER BY id) FROM customer_lists WHERE COALESCE(organization_id,0)=$1),'') || ':' ||
 COALESCE((SELECT string_agg(key || value::text,',' ORDER BY key) FROM settings WHERE key IN ('privacy.individual_tracking','privacy.disable_tracking')),'') || ':' || $2::text)`, orgID, admin).Scan(&a.IndividualTracking, &a.TrackingDisabled, &stamp)
	return a, stamp, err
}

func SafeFilename(name string) string {
	s := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	r := []rune(s)
	if len(r) > 100 {
		s = string(r[:100])
	}
	return strings.Trim(s, " .")
}

func (s *Service) Create(ctx context.Context, a Access, r Request, orgName string) (Job, error) {
	var job Job
	if err := r.Validate(); err != nil {
		return job, err
	}
	if _, _, err := BuildQuery(r, a); err != nil {
		return job, err
	}
	b, err := json.Marshal(r)
	if err != nil {
		return job, err
	}
	filename := SafeFilename(Types[r.Type]+"_"+time.Now().In(time.FixedZone("CST", 8*3600)).Format("20060102_150405")+"_"+orgName) + "." + r.Format
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return job, err
	}
	defer tx.Rollback()
	// Serialize per-user queue limits across concurrent HTTP requests.
	if _, err = tx.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, a.UserID); err != nil {
		return job, err
	}
	var n int
	if err = tx.GetContext(ctx, &n, `SELECT count(*) FROM data_export_jobs WHERE user_id=$1 AND status IN ('pending','running')`, a.UserID); err != nil {
		return job, err
	}
	if n >= 3 {
		return job, fmt.Errorf("最多同时保留 3 个未完成导出任务，请等待后重试")
	}
	err = tx.GetContext(ctx, &job, `INSERT INTO data_export_jobs(user_id,organization_id,request,filename) VALUES($1,$2,$3,$4) RETURNING `+jobColumns, a.UserID, a.OrganizationID, string(b), filename)
	if err != nil {
		return job, err
	}
	return job, tx.Commit()
}

func (s *Service) List(ctx context.Context, a Access, page int) ([]Job, error) {
	jobs := []Job{}
	err := s.DB.SelectContext(ctx, &jobs, `SELECT `+jobColumns+` FROM data_export_jobs WHERE user_id=$1 AND organization_id=$2 ORDER BY created_at DESC LIMIT 30 OFFSET $3`, a.UserID, a.OrganizationID, (page-1)*30)
	return jobs, err
}

// Run uses a session advisory lock so multiple application instances cannot
// process the queue concurrently. A crashed process releases the lock; its job
// is retried by the next worker, without exposing a partially written artifact.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.tick(ctx); err != nil && ctx.Err() == nil {
				s.Log.Printf("export worker: %v", err)
			}
		}
	}
}
func (s *Service) tick(ctx context.Context) error {
	conn, err := s.DB.Connx(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked bool
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(62800909)`).Scan(&locked); err != nil || !locked {
		return err
	}
	defer conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(62800909)`)
	if _, err = conn.ExecContext(ctx, `WITH expired AS (UPDATE data_export_jobs SET status='expired' WHERE expires_at<=NOW() AND status<>'expired' RETURNING id) DELETE FROM data_export_chunks WHERE job_id IN (SELECT id FROM expired)`); err != nil {
		return err
	}
	var job Job
	err = conn.GetContext(ctx, &job, `UPDATE data_export_jobs SET status='running',started_at=NOW(),row_count=0,error='' WHERE id=(SELECT id FROM data_export_jobs WHERE status IN ('pending','running') AND expires_at>NOW() ORDER BY created_at LIMIT 1) RETURNING `+jobColumns)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	jobCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	if err = s.generate(jobCtx, job); err != nil {
		// Never put SQL errors or source data into user-visible error messages.
		s.Log.Printf("export %s failed: %v", job.ID, err)
		_, updateErr := conn.ExecContext(ctx, `UPDATE data_export_jobs SET status='failed',error=$2,completed_at=NOW() WHERE id=$1`, job.ID, "导出失败：权限、追踪设置发生变化，数据过大或查询失败。请缩小范围重新生成；管理员可按任务编号查日志。")
		return updateErr
	}
	return nil
}

func (s *Service) generate(ctx context.Context, job Job) error {
	a, stamp, err := s.Access(ctx, job.UserID, job.OrganizationID)
	if err != nil {
		return err
	}
	var req Request
	if err = json.Unmarshal(job.Request, &req); err != nil {
		return err
	}
	query, args, err := BuildQuery(req, a)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "listmonk-export-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	tx, err := s.DB.BeginTxx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryxContext(ctx, query, args...)
	if err != nil {
		return err
	}
	count, err := WriteRows(ctx, file, req.Format, rows, func(n int64) {
		s.DB.ExecContext(ctx, `UPDATE data_export_jobs SET row_count=$2 WHERE id=$1`, job.ID, n)
	})
	rows.Close()
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, current, err := s.Access(ctx, job.UserID, job.OrganizationID)
	if err != nil {
		return err
	}
	if current != stamp {
		return fmt.Errorf("export privacy changed")
	}
	writeTx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer writeTx.Rollback()
	if _, err = writeTx.ExecContext(ctx, `DELETE FROM data_export_chunks WHERE job_id=$1`, job.ID); err != nil {
		return err
	}
	buf := make([]byte, 1024*1024)
	for seq := 0; ; seq++ {
		n, e := file.Read(buf)
		if n > 0 {
			if _, err = writeTx.ExecContext(ctx, `INSERT INTO data_export_chunks(job_id,sequence,content) VALUES($1,$2,$3)`, job.ID, seq, buf[:n]); err != nil {
				return err
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
	}
	_, err = writeTx.ExecContext(ctx, `UPDATE data_export_jobs SET status='complete',completed_at=NOW(),expires_at=NOW()+INTERVAL '7 days',row_count=$2,access_stamp=$3 WHERE id=$1`, job.ID, count, stamp)
	if err != nil {
		return err
	}
	return writeTx.Commit()
}

// SafeCSVCell prevents spreadsheet applications from evaluating untrusted text.
func SafeCSVCell(s string) string {
	t := strings.TrimLeftFunc(s, unicode.IsSpace)
	if t != "" && strings.ContainsRune("=+-@", []rune(t)[0]) {
		return "'" + s
	}
	return s
}

func WriteRows(ctx context.Context, out io.Writer, format string, rows *sqlx.Rows, progress func(int64)) (int64, error) {
	head, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	var cw *csv.Writer
	var book *excelize.File
	var sw *excelize.StreamWriter
	if format == "csv" {
		if _, err = io.WriteString(out, "\xef\xbb\xbf"); err != nil {
			return 0, err
		}
		cw = csv.NewWriter(out)
		if err = cw.Write(head); err != nil {
			return 0, err
		}
	} else {
		book = excelize.NewFile()
		defer book.Close()
		sw, err = book.NewStreamWriter("Sheet1")
		if err != nil {
			return 0, err
		}
		h := make([]any, len(head))
		for i, s := range head {
			h[i] = s
		}
		if err = sw.SetRow("A1", h); err != nil {
			return 0, err
		}
	}
	var count int64
	var rawBytes int64
	for rows.Next() {
		if err = ctx.Err(); err != nil {
			return count, err
		}
		vals, e := rows.SliceScan()
		if e != nil {
			return count, e
		}
		texts := make([]string, len(vals))
		for i, v := range vals {
			switch x := v.(type) {
			case nil:
				texts[i] = ""
			case time.Time:
				texts[i] = x.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
			case []byte:
				texts[i] = string(x)
			default:
				texts[i] = fmt.Sprint(x)
			}
			rawBytes += int64(len(texts[i]))
		}
		count++
		if count > 1000000 || rawBytes > 256*1024*1024 {
			return count, fmt.Errorf("export exceeds 1 million rows or 256 MiB; narrow filters")
		}
		if cw != nil {
			for i := range texts {
				texts[i] = SafeCSVCell(texts[i])
			}
			err = cw.Write(texts)
		} else {
			cells := make([]any, len(texts))
			for i, s := range texts {
				if len([]rune(s)) > 32767 {
					return count, fmt.Errorf("Excel cell exceeds 32767 characters; use CSV")
				}
				cells[i] = s
			}
			err = sw.SetRow(fmt.Sprintf("A%d", count+1), cells)
		}
		if err != nil {
			return count, err
		}
		if count%1000 == 0 && progress != nil {
			progress(count)
		}
	}
	if err = rows.Err(); err != nil {
		return count, err
	}
	if cw != nil {
		cw.Flush()
		return count, cw.Error()
	}
	if err = sw.Flush(); err != nil {
		return count, err
	}
	return count, book.Write(out)
}

// Download checks both the current administrator and the artifact's privacy
// stamp before streaming chunks. Jobs are private to their requesting admin.
func (s *Service) Download(ctx context.Context, a Access, id string) (Job, *sqlx.Rows, error) {
	var job Job
	err := s.DB.GetContext(ctx, &job, `SELECT `+jobColumns+` FROM data_export_jobs WHERE id=$1 AND user_id=$2 AND organization_id=$3 AND status='complete' AND expires_at>NOW()`, id, a.UserID, a.OrganizationID)
	if err != nil {
		return job, nil, err
	}
	_, stamp, err := s.Access(ctx, a.UserID, a.OrganizationID)
	if err != nil {
		return job, nil, err
	}
	var saved string
	if err = s.DB.GetContext(ctx, &saved, `SELECT access_stamp FROM data_export_jobs WHERE id=$1`, id); err != nil {
		return job, nil, err
	}
	if saved != stamp {
		return job, nil, fmt.Errorf("权限或邮箱打码设置已改变，请重新生成文件")
	}
	rows, err := s.DB.QueryxContext(ctx, `SELECT content FROM data_export_chunks WHERE job_id=$1 ORDER BY sequence`, id)
	return job, rows, err
}
