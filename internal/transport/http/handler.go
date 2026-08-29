package httptransport

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lihongjie0209/audit-service/internal/apperror"
	auditdomain "github.com/lihongjie0209/audit-service/internal/audit"
	"github.com/lihongjie0209/audit-service/internal/buildinfo"
	"github.com/lihongjie0209/audit-service/internal/health"
)

type Handler struct {
	logger *slog.Logger
	health *health.Service

	audits *auditdomain.Service
}

func NewHandler(healthService *health.Service, auditService *auditdomain.Service, logger *slog.Logger) *Handler {
	return &Handler{health: healthService, audits: auditService, logger: logger}
}

type RecordAuditRequest struct {
	ID            string          `json:"id"`
	TenantID      string          `json:"tenant_id" binding:"required"`
	ActorID       string          `json:"actor_id"`
	ActorType     string          `json:"actor_type"`
	Action        string          `json:"action" binding:"required"`
	ResourceType  string          `json:"resource_type" binding:"required"`
	ResourceID    string          `json:"resource_id" binding:"required"`
	RequestID     string          `json:"request_id"`
	TraceID       string          `json:"trace_id"`
	SourceService string          `json:"source_service" binding:"required"`
	BeforeSummary json.RawMessage `json:"before_summary"`
	AfterSummary  json.RawMessage `json:"after_summary"`
	OccurredAt    time.Time       `json:"occurred_at"`
}
type GetAuditRequest struct {
	ID       string `json:"id" binding:"required"`
	TenantID string `json:"tenant_id" binding:"required"`
}
type QueryAuditRequest struct {
	TenantID     string    `json:"tenant_id" binding:"required"`
	ActorID      string    `json:"actor_id"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	RequestID    string    `json:"request_id"`
	OccurredFrom time.Time `json:"occurred_from"`
	OccurredTo   time.Time `json:"occurred_to"`
	Page         int       `json:"page"`
	PageSize     int       `json:"page_size"`
}
type ExportAuditRequest struct {
	QueryAuditRequest
	MaxRecords int `json:"max_records"`
}

type MeResponseBody struct {
	Subject string `json:"subject"`
}

// Login godoc
// @Summary Issue a JWT access token
// @Tags authentication
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Client credentials"
// @Success 200 {object} Response{body=LoginResponseBody}
// @Failure 400 {object} Response "Code 10001: invalid request"
// @Failure 401 {object} Response "Code 20001: invalid credentials"
// @Failure 429 {object} Response "Code 10029: rate limited"

// Live godoc
// @Summary Check process liveness
// @Tags operations
// @Produce json
// @Success 200 {object} Response{body=health.Status}
// @Router /live [post]
func (h *Handler) Live(c *gin.Context) { OK(c, h.health.Live()) }

// Ready godoc
// @Summary Check database and Redis readiness
// @Tags operations
// @Produce json
// @Success 200 {object} Response{body=health.Status}
// @Failure 503 {object} Response{body=health.Status} "Code 50003: dependency unavailable"
// @Router /ready [post]
func (h *Handler) Ready(c *gin.Context) {
	status, ready := h.health.Ready(c.Request.Context())
	if !ready {
		c.JSON(503, Response{Code: apperror.CodeDependencyUnavailable, Message: "service is not ready", Body: status, RequestID: requestID(c)})
		return
	}
	OK(c, status)
}

// Me godoc
// @Summary Return the authenticated subject
// @Tags authentication
// @Produce json
// @Security Bearer
// @Success 200 {object} Response{body=MeResponseBody}
// @Failure 401 {object} Response "Code 20001: unauthorized"
// @Router /api/v1/me [post]
func (h *Handler) Me(c *gin.Context) {
	subject, _ := c.Get("subject")
	OK(c, gin.H{"subject": subject})
}

// Version godoc
// @Summary Return build and runtime version information
// @Tags operations
// @Produce json
// @Success 200 {object} Response{body=buildinfo.Info}
// @Router /api/v1/version [post]
func (h *Handler) Version(c *gin.Context) { OK(c, buildinfo.Current()) }

// RecordAudit godoc
// @Summary Record an immutable business audit entry
// @Tags audit
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body RecordAuditRequest true "Audit record"
// @Success 200 {object} Response{body=audit.Record}
// @Router /api/v1/audit/records/create [post]
func (h *Handler) RecordAudit(c *gin.Context) {
	var request RecordAuditRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	created, err := h.audits.Record(c.Request.Context(), auditdomain.Record{ID: request.ID, TenantID: request.TenantID, ActorID: request.ActorID, ActorType: request.ActorType, Action: request.Action, ResourceType: request.ResourceType, ResourceID: request.ResourceID, RequestID: request.RequestID, TraceID: request.TraceID, SourceService: request.SourceService, BeforeSummary: request.BeforeSummary, AfterSummary: request.AfterSummary, OccurredAt: request.OccurredAt})
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, created)
}

// GetAudit godoc
// @Summary Get an audit record
// @Tags audit
// @Security Bearer
// @Param request body GetAuditRequest true "Audit ID"
// @Success 200 {object} Response{body=audit.Record}
// @Router /api/v1/audit/records/get [post]
func (h *Handler) GetAudit(c *gin.Context) {
	var request GetAuditRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.audits.Get(c.Request.Context(), request.ID, request.TenantID)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, value)
}

// QueryAudits godoc
// @Summary Query audit records
// @Tags audit
// @Security Bearer
// @Param request body QueryAuditRequest true "Audit filter"
// @Success 200 {object} Response{body=audit.Page}
// @Router /api/v1/audit/records/query [post]
func (h *Handler) QueryAudits(c *gin.Context) {
	var request QueryAuditRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	value, err := h.audits.Query(c.Request.Context(), auditdomain.Filter{TenantID: request.TenantID, ActorID: request.ActorID, Action: request.Action, ResourceType: request.ResourceType, ResourceID: request.ResourceID, RequestID: request.RequestID, OccurredFrom: request.OccurredFrom, OccurredTo: request.OccurredTo, Page: request.Page, PageSize: request.PageSize})
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, value)
}

// ExportAudits godoc
// @Summary Export a bounded audit query as CSV
// @Tags audit
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ExportAuditRequest true "Audit export filter"
// @Success 200 {object} Response
// @Router /api/v1/audit/records/export [post]
func (h *Handler) ExportAudits(c *gin.Context) {
	var request ExportAuditRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid json request", err))
		return
	}
	filter := auditdomain.Filter{TenantID: request.TenantID, ActorID: request.ActorID, Action: request.Action, ResourceType: request.ResourceType, ResourceID: request.ResourceID, RequestID: request.RequestID, OccurredFrom: request.OccurredFrom, OccurredTo: request.OccurredTo}
	content, count, err := h.audits.Export(c.Request.Context(), filter, request.MaxRecords)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, gin.H{"content": content, "content_type": "text/csv; charset=utf-8", "filename": "audit-records.csv", "record_count": count})
}

// CreateUser godoc
// @Summary Create a user
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body CreateUserRequest true "User"
// @Success 200 {object} Response{body=user.User}
// @Failure 400 {object} Response "Code 10001: invalid request"
// @Failure 409 {object} Response "Code 30009: email already exists"

// GetUser godoc
// @Summary Get a user
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body GetUserRequest true "User ID"
// @Success 200 {object} Response{body=user.User}
// @Failure 404 {object} Response "Code 10004: user not found"

// ListUsers godoc
// @Summary List users
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body ListUsersRequest true "Pagination"
// @Success 200 {object} Response{body=user.Page}

// UpdateUser godoc
// @Summary Update a user using optimistic locking
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body UpdateUserRequest true "User and current version"
// @Success 200 {object} Response{body=user.User}
// @Failure 409 {object} Response "Code 30009: version conflict"

// DeleteUser godoc
// @Summary Delete a user using optimistic locking
// @Tags users
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body DeleteUserRequest true "User ID and current version"
// @Success 200 {object} Response
