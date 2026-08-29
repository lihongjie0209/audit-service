package auth

import (
	"testing"
	"time"

	"github.com/lihongjie0209/audit-service/internal/config"
)

func TestService_IssueAndParse(t *testing.T) {
	t.Parallel()
	service := New(config.Config{JWT: config.JWT{Issuer: "test", Secret: "01234567890123456789012345678901", TTL: time.Hour}, Auth: config.Auth{ClientID: "client", ClientSecret: "secret"}})
	token, err := service.Issue("client")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	claims, err := service.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if claims.Subject != "client" {
		t.Fatalf("Subject = %q, want client", claims.Subject)
	}
}

func TestService_Authenticate(t *testing.T) {
	t.Parallel()
	service := New(config.Config{JWT: config.JWT{Secret: "01234567890123456789012345678901"}, Auth: config.Auth{ClientID: "client", ClientSecret: "secret"}})
	if !service.Authenticate("client", "secret") {
		t.Fatal("Authenticate() = false, want true")
	}
	if service.Authenticate("client", "wrong") {
		t.Fatal("Authenticate() = true, want false")
	}
}
