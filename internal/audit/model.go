package audit

import "time"

type Record struct {
	ID            string    `db:"id" json:"id"`
	TenantID      string    `db:"tenant_id" json:"tenant_id"`
	ActorID       string    `db:"actor_id" json:"actor_id"`
	ActorType     string    `db:"actor_type" json:"actor_type"`
	Action        string    `db:"action" json:"action"`
	ResourceType  string    `db:"resource_type" json:"resource_type"`
	ResourceID    string    `db:"resource_id" json:"resource_id"`
	RequestID     string    `db:"request_id" json:"request_id"`
	TraceID       string    `db:"trace_id" json:"trace_id"`
	SourceService string    `db:"source_service" json:"source_service"`
	BeforeSummary []byte    `db:"before_summary" json:"before_summary"`
	AfterSummary  []byte    `db:"after_summary" json:"after_summary"`
	OccurredAt    time.Time `db:"occurred_at" json:"occurred_at"`
	Version       int64     `db:"version" json:"version"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time `db:"updated_at" json:"updated_at"`
	CreatedBy     string    `db:"created_by" json:"created_by"`
	UpdatedBy     string    `db:"updated_by" json:"updated_by"`
}

type Filter struct {
	TenantID      string
	ActorID       string
	ActorType     string
	Action        string
	ResourceType  string
	ResourceID    string
	RequestID     string
	TraceID       string
	SourceService string
	OccurredFrom  time.Time
	OccurredTo    time.Time
	Page          int
	PageSize      int
}

type Page struct {
	Records  []Record `json:"records"`
	Total    int64    `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
}
