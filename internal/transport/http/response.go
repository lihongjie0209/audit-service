package httptransport

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lihongjie0209/audit-service/internal/apperror"
	"go.opentelemetry.io/otel/trace"
)

type Response struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Body      any    `json:"body"`
	RequestID string `json:"request_id,omitempty"`
}

func OK(c *gin.Context, body any) {
	c.JSON(http.StatusOK, Response{Code: apperror.CodeOK, Message: "success", Body: body, RequestID: requestID(c)})
}
func Fail(c *gin.Context, logger *slog.Logger, err error) {
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		appErr = apperror.Internal(err)
	}
	if appErr.HTTPStatus >= 500 {
		span := trace.SpanFromContext(c.Request.Context()).SpanContext()
		logger.ErrorContext(c.Request.Context(), "request failed", "error", appErr.Err, "request_id", requestID(c), "trace_id", span.TraceID().String(), "span_id", span.SpanID().String())
	}
	c.AbortWithStatusJSON(appErr.HTTPStatus, Response{Code: appErr.Code, Message: appErr.Message, Body: nil, RequestID: requestID(c)})
}
