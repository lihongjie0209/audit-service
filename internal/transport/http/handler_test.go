package httptransport

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	auditdomain "github.com/lihongjie0209/audit-service/internal/audit"
)

func TestAuditRecordResponsePreservesJSONSummaries(t *testing.T) {
	t.Parallel()
	response := auditRecordResponse(auditdomain.Record{
		ID:            "audit-1",
		BeforeSummary: []byte(`{"status":"draft"}`),
		AfterSummary:  []byte(`{"status":"published"}`),
	})
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"before_summary":{"status":"draft"}`)) {
		t.Fatalf("response encoded summary as non-JSON data: %s", encoded)
	}
	if !bytes.Contains(encoded, []byte(`"after_summary":{"status":"published"}`)) {
		t.Fatalf("response encoded summary as non-JSON data: %s", encoded)
	}
}

func TestAuditFilterMapsCorrelationAndSourceFields(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	filter := auditFilter(QueryAuditRequest{
		TenantID:      "tenant-1",
		ApplicationID: "application-1",
		ActorID:       "user-1",
		ActorType:     "user",
		Action:        "invoice.finalized",
		ResourceType:  "invoice",
		ResourceID:    "invoice-1",
		RequestID:     "request-1",
		TraceID:       "trace-1",
		SourceService: "billing-service",
		OccurredFrom:  from,
		OccurredTo:    to,
		Page:          2,
		PageSize:      50,
	})
	if filter.ApplicationID != "application-1" || filter.ActorType != "user" || filter.TraceID != "trace-1" || filter.SourceService != "billing-service" {
		t.Fatalf("auditFilter() = %#v", filter)
	}
	if filter.OccurredFrom != from || filter.OccurredTo != to || filter.Page != 2 || filter.PageSize != 50 {
		t.Fatalf("auditFilter() pagination/time = %#v", filter)
	}
}
