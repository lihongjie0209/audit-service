package app

import (
	"encoding/json"
	"strings"
	"testing"

	commonv1 "github.com/lihongjie0209/platform-protos/gen/go/platform/common/v1"
)

func TestSummaryFromEnvelopePreservesObservablePayloadAndRedactsSecrets(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"operation":"tenant.update","duration_ms":37,"succeeded":false,"error_message":"conflict","client_ip":"192.0.2.1","user_agent":"test-agent","password":"must-not-survive"}`)
	summary, err := summaryFromEnvelope(&commonv1.EventEnvelope{EventType: "platform.operation-log.recorded.v1", SchemaVersion: 1, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(summary) {
		t.Fatalf("summary is not JSON: %s", summary)
	}
	text := string(summary)
	for _, want := range []string{"tenant.update", "duration_ms", "succeeded", "client_ip", "user_agent"} {
		if !strings.Contains(text, want) {
			t.Fatalf("summary %s does not contain %q", text, want)
		}
	}
	if strings.Contains(text, "must-not-survive") {
		t.Fatalf("summary leaked secret: %s", text)
	}
}

func TestSummaryFromEnvelopeKeepsMetadataForBinaryDomainPayload(t *testing.T) {
	t.Parallel()
	summary, err := summaryFromEnvelope(&commonv1.EventEnvelope{EventType: "platform.tenant.changed.v1", SchemaVersion: 3, Payload: []byte{0xff, 0x01}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(summary), "payload") || !strings.Contains(string(summary), `"schema_version":3`) {
		t.Fatalf("summary = %s", summary)
	}
}

func TestSourceServiceUsesOperationProducer(t *testing.T) {
	t.Parallel()
	envelope := &commonv1.EventEnvelope{EventType: "platform.operation-log.recorded.v1", Payload: []byte(`{"source":"tenant-service"}`)}
	if got := sourceService(envelope); got != "tenant-service" {
		t.Fatalf("source service = %q", got)
	}
}
