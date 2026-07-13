package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

const (
	exportJobStatusQueued    = "queued"
	exportJobStatusRunning   = "running"
	exportJobStatusSucceeded = "succeeded"
	exportJobStatusFailed    = "failed"
)

type csvExportJob struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Status      string            `json:"status"`
	Filename    string            `json:"filename"`
	ContentType string            `json:"content_type"`
	RowCount    int               `json:"row_count"`
	SizeBytes   int               `json:"size_bytes"`
	Error       string            `json:"error"`
	Parameters  map[string]string `json:"parameters"`
	Owner       string            `json:"owner"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	ExpiresAt   time.Time         `json:"expires_at"`
}

type CSVExportJobStore interface {
	create(ctx context.Context, owner string, kind string, filename string, parameters map[string]string) (csvExportJob, error)
	markRunning(ctx context.Context, id string) error
	markSucceeded(ctx context.Context, id string, rowCount int, body []byte) error
	markFailed(ctx context.Context, id string, err error) error
	get(ctx context.Context, id string) (csvExportJob, bool, error)
	getDownload(ctx context.Context, id string) (csvExportJob, []byte, bool, error)
	list(ctx context.Context, limit int) ([]csvExportJob, error)
	Health(ctx context.Context) error
	Close() error
}

type csvExportJobRecord struct {
	job  csvExportJob
	body []byte
}

type memoryCSVExportJobStore struct {
	mu   sync.RWMutex
	jobs map[string]csvExportJobRecord
}

func NewCSVExportJobStore(ctx context.Context, databaseURL string) (CSVExportJobStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return newCSVExportJobStore(), nil
	}
	return newPostgresCSVExportJobStore(ctx, databaseURL)
}

func newCSVExportJobStore() *memoryCSVExportJobStore {
	return &memoryCSVExportJobStore{jobs: map[string]csvExportJobRecord{}}
}

func (s *memoryCSVExportJobStore) create(_ context.Context, owner string, kind string, filename string, parameters map[string]string) (csvExportJob, error) {
	now := time.Now().UTC()
	job := csvExportJob{
		ID:          "export_" + randomExportID(),
		Kind:        kind,
		Status:      exportJobStatusQueued,
		Filename:    filename,
		ContentType: "text/csv; charset=utf-8",
		Parameters:  cloneStringMap(parameters),
		Owner:       strings.TrimSpace(owner),
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   now.Add(24 * time.Hour),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now)
	s.jobs[job.ID] = csvExportJobRecord{job: job}
	return job, nil
}

func (s *memoryCSVExportJobStore) markRunning(_ context.Context, id string) error {
	s.update(id, func(record csvExportJobRecord) csvExportJobRecord {
		record.job.Status = exportJobStatusRunning
		record.job.UpdatedAt = time.Now().UTC()
		return record
	})
	return nil
}

func (s *memoryCSVExportJobStore) markSucceeded(_ context.Context, id string, rowCount int, body []byte) error {
	s.update(id, func(record csvExportJobRecord) csvExportJobRecord {
		record.job.Status = exportJobStatusSucceeded
		record.job.RowCount = rowCount
		record.job.SizeBytes = len(body)
		record.job.Error = ""
		record.job.UpdatedAt = time.Now().UTC()
		record.body = append([]byte(nil), body...)
		return record
	})
	return nil
}

func (s *memoryCSVExportJobStore) markFailed(_ context.Context, id string, err error) error {
	s.update(id, func(record csvExportJobRecord) csvExportJobRecord {
		record.job.Status = exportJobStatusFailed
		if err != nil {
			record.job.Error = err.Error()
		}
		record.job.UpdatedAt = time.Now().UTC()
		return record
	})
	return nil
}

func (s *memoryCSVExportJobStore) update(id string, apply func(csvExportJobRecord) csvExportJobRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.jobs[id]
	if !ok {
		return
	}
	s.jobs[id] = apply(record)
}

func (s *memoryCSVExportJobStore) get(_ context.Context, id string) (csvExportJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.jobs[id]
	if !ok {
		return csvExportJob{}, false, nil
	}
	if time.Now().UTC().After(record.job.ExpiresAt) {
		delete(s.jobs, id)
		return csvExportJob{}, false, nil
	}
	return record.job, true, nil
}

func (s *memoryCSVExportJobStore) getDownload(_ context.Context, id string) (csvExportJob, []byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.jobs[id]
	if !ok || record.job.Status != exportJobStatusSucceeded {
		return csvExportJob{}, nil, false, nil
	}
	if time.Now().UTC().After(record.job.ExpiresAt) {
		delete(s.jobs, id)
		return csvExportJob{}, nil, false, nil
	}
	return record.job, append([]byte(nil), record.body...), true, nil
}

func (s *memoryCSVExportJobStore) list(_ context.Context, limit int) ([]csvExportJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(time.Now().UTC())
	out := make([]csvExportJob, 0, len(s.jobs))
	for _, record := range s.jobs {
		out = append(out, record.job)
	}
	sortExportJobs(out)
	if len(out) > limit {
		return out[:limit], nil
	}
	return out, nil
}

func (s *memoryCSVExportJobStore) Health(context.Context) error {
	return nil
}

func (s *memoryCSVExportJobStore) Close() error {
	return nil
}

func (s *memoryCSVExportJobStore) pruneExpiredLocked(now time.Time) {
	for id, record := range s.jobs {
		if now.After(record.job.ExpiresAt) {
			delete(s.jobs, id)
		}
	}
}

type postgresCSVExportJobStore struct {
	db *sql.DB
}

func newPostgresCSVExportJobStore(ctx context.Context, databaseURL string) (*postgresCSVExportJobStore, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	store := &postgresCSVExportJobStore{db: db}
	if err := store.Health(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *postgresCSVExportJobStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS csv_export_jobs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  filename TEXT NOT NULL,
  content_type TEXT NOT NULL,
  row_count INTEGER NOT NULL DEFAULT 0,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  parameters TEXT NOT NULL DEFAULT '{}',
  body BYTEA,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE csv_export_jobs ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS csv_export_jobs_created_idx
  ON csv_export_jobs(created_at DESC);

CREATE INDEX IF NOT EXISTS csv_export_jobs_expires_idx
  ON csv_export_jobs(expires_at);

CREATE INDEX IF NOT EXISTS csv_export_jobs_owner_created_idx
  ON csv_export_jobs(owner, created_at DESC);
`)
	return err
}

func (s *postgresCSVExportJobStore) create(ctx context.Context, owner string, kind string, filename string, parameters map[string]string) (csvExportJob, error) {
	now := time.Now().UTC()
	job := csvExportJob{
		ID:          "export_" + randomExportID(),
		Kind:        kind,
		Status:      exportJobStatusQueued,
		Filename:    filename,
		ContentType: "text/csv; charset=utf-8",
		Parameters:  cloneStringMap(parameters),
		Owner:       strings.TrimSpace(owner),
		CreatedAt:   now,
		UpdatedAt:   now,
		ExpiresAt:   now.Add(24 * time.Hour),
	}
	if err := s.pruneExpired(ctx, now); err != nil {
		return csvExportJob{}, err
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO csv_export_jobs(id, owner, kind, status, filename, content_type, row_count, size_bytes, error, parameters, body, created_at, updated_at, expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
`, job.ID, job.Owner, job.Kind, job.Status, job.Filename, job.ContentType, job.RowCount, job.SizeBytes, job.Error, marshalStringMap(job.Parameters), []byte(nil), job.CreatedAt, job.UpdatedAt, job.ExpiresAt)
	return job, err
}

func (s *postgresCSVExportJobStore) markRunning(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE csv_export_jobs
SET status = $1, updated_at = $2
WHERE id = $3
`, exportJobStatusRunning, time.Now().UTC(), id)
	return err
}

func (s *postgresCSVExportJobStore) markSucceeded(ctx context.Context, id string, rowCount int, body []byte) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
UPDATE csv_export_jobs
SET status = $1, row_count = $2, size_bytes = $3, error = '', body = $4, updated_at = $5
WHERE id = $6
`, exportJobStatusSucceeded, rowCount, len(body), body, now, id)
	return err
}

func (s *postgresCSVExportJobStore) markFailed(ctx context.Context, id string, runErr error) error {
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE csv_export_jobs
SET status = $1, error = $2, updated_at = $3
WHERE id = $4
`, exportJobStatusFailed, message, time.Now().UTC(), id)
	return err
}

func (s *postgresCSVExportJobStore) get(ctx context.Context, id string) (csvExportJob, bool, error) {
	if err := s.pruneExpired(ctx, time.Now().UTC()); err != nil {
		return csvExportJob{}, false, err
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, owner, kind, status, filename, content_type, row_count, size_bytes, error, parameters, created_at, updated_at, expires_at
FROM csv_export_jobs
WHERE id = $1
`, id)
	return scanCSVExportJob(row)
}

func (s *postgresCSVExportJobStore) getDownload(ctx context.Context, id string) (csvExportJob, []byte, bool, error) {
	if err := s.pruneExpired(ctx, time.Now().UTC()); err != nil {
		return csvExportJob{}, nil, false, err
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, owner, kind, status, filename, content_type, row_count, size_bytes, error, parameters, created_at, updated_at, expires_at, body
FROM csv_export_jobs
WHERE id = $1 AND status = $2
`, id, exportJobStatusSucceeded)
	job, body, ok, err := scanCSVExportJobWithBody(row)
	if err != nil || !ok {
		return csvExportJob{}, nil, ok, err
	}
	if body == nil {
		return csvExportJob{}, nil, false, nil
	}
	return job, append([]byte(nil), body...), true, nil
}

func (s *postgresCSVExportJobStore) list(ctx context.Context, limit int) ([]csvExportJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if err := s.pruneExpired(ctx, time.Now().UTC()); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, owner, kind, status, filename, content_type, row_count, size_bytes, error, parameters, created_at, updated_at, expires_at
FROM csv_export_jobs
ORDER BY created_at DESC
LIMIT $1
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []csvExportJob{}
	for rows.Next() {
		job, err := scanCSVExportJobColumns(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *postgresCSVExportJobStore) Health(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *postgresCSVExportJobStore) Close() error {
	return s.db.Close()
}

func (s *postgresCSVExportJobStore) pruneExpired(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM csv_export_jobs WHERE expires_at < $1`, now)
	return err
}

type csvExportJobScanner interface {
	Scan(dest ...any) error
}

func scanCSVExportJob(row csvExportJobScanner) (csvExportJob, bool, error) {
	job, err := scanCSVExportJobColumns(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return csvExportJob{}, false, nil
		}
		return csvExportJob{}, false, err
	}
	return job, true, nil
}

func scanCSVExportJobWithBody(row csvExportJobScanner) (csvExportJob, []byte, bool, error) {
	var job csvExportJob
	var parameters string
	var body []byte
	if err := row.Scan(&job.ID, &job.Owner, &job.Kind, &job.Status, &job.Filename, &job.ContentType, &job.RowCount, &job.SizeBytes, &job.Error, &parameters, &job.CreatedAt, &job.UpdatedAt, &job.ExpiresAt, &body); err != nil {
		if err == sql.ErrNoRows {
			return csvExportJob{}, nil, false, nil
		}
		return csvExportJob{}, nil, false, err
	}
	job.Parameters = parseStringMap(parameters)
	return job, body, true, nil
}

func scanCSVExportJobColumns(row csvExportJobScanner) (csvExportJob, error) {
	var job csvExportJob
	var parameters string
	if err := row.Scan(&job.ID, &job.Owner, &job.Kind, &job.Status, &job.Filename, &job.ContentType, &job.RowCount, &job.SizeBytes, &job.Error, &parameters, &job.CreatedAt, &job.UpdatedAt, &job.ExpiresAt); err != nil {
		return csvExportJob{}, err
	}
	job.Parameters = parseStringMap(parameters)
	return job, nil
}

func sortExportJobs(jobs []csvExportJob) {
	for i := 1; i < len(jobs); i++ {
		current := jobs[i]
		j := i - 1
		for j >= 0 && jobs[j].CreatedAt.Before(current.CreatedAt) {
			jobs[j+1] = jobs[j]
			j--
		}
		jobs[j+1] = current
	}
}

func randomExportID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func marshalStringMap(values map[string]string) string {
	if len(values) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func parseStringMap(value string) map[string]string {
	if strings.TrimSpace(value) == "" {
		return map[string]string{}
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return map[string]string{}
	}
	return out
}
