package httptransport

import (
	"github.com/gin-gonic/gin"
	"github.com/lihongjie0209/audit-service/internal/apperror"
	"github.com/lihongjie0209/audit-service/internal/routepolicy"
)

type PageRoutePoliciesRequest struct {
	Keyword      string `json:"keyword"`
	Protocol     string `json:"protocol"`
	RouteStatus  string `json:"route_status"`
	PolicyStatus string `json:"policy_status"`
	Page         int    `json:"page"`
	PageSize     int    `json:"page_size"`
}

type GetRoutePolicyRequest struct {
	RouteID string `json:"route_id" binding:"required"`
}

type SetRoutePolicyRequest struct {
	RouteID         string                        `json:"route_id" binding:"required"`
	Expression      string                        `json:"expression" binding:"required"`
	Description     string                        `json:"description"`
	Status          string                        `json:"status" binding:"required"`
	ExpectedVersion int64                         `json:"expected_version"`
	Permissions     []routepolicy.PermissionInput `json:"permissions"`
}

type RoutePolicyPage struct {
	Items    []routepolicy.Detail `json:"items"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

// PageRoutePolicies godoc
// @Summary Page discovered routes and their database-owned authorization policies
// @Tags route-policies
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body PageRoutePoliciesRequest true "Route policy filters"
// @Success 200 {object} Response{body=RoutePolicyPage}
// @Router /api/v1/route-policies/page [post]
func (h *Handler) PageRoutePolicies(c *gin.Context) {
	var request PageRoutePoliciesRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid request", err))
		return
	}
	if request.Page == 0 {
		request.Page = 1
	}
	if request.PageSize == 0 {
		request.PageSize = 20
	}
	items, total, err := h.routePolicies.Page(c.Request.Context(), routepolicy.Filter{Keyword: request.Keyword, Protocol: request.Protocol, RouteStatus: request.RouteStatus, PolicyStatus: request.PolicyStatus}, request.Page, request.PageSize)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, RoutePolicyPage{Items: items, Total: total, Page: request.Page, PageSize: request.PageSize})
}

// GetRoutePolicy godoc
// @Summary Get one discovered route and its authorization policy
// @Tags route-policies
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body GetRoutePolicyRequest true "Route identifier"
// @Success 200 {object} Response{body=routepolicy.Detail}
// @Router /api/v1/route-policies/get [post]
func (h *Handler) GetRoutePolicy(c *gin.Context) {
	var request GetRoutePolicyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid request", err))
		return
	}
	detail, err := h.routePolicies.Get(c.Request.Context(), request.RouteID)
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, detail)
}

// SetRoutePolicy godoc
// @Summary Create or update a route authorization policy
// @Tags route-policies
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body SetRoutePolicyRequest true "Compiled CEL policy and permission references"
// @Success 200 {object} Response{body=routepolicy.Detail}
// @Router /api/v1/route-policies/set [post]
func (h *Handler) SetRoutePolicy(c *gin.Context) {
	var request SetRoutePolicyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		Fail(c, h.logger, apperror.Invalid("invalid request", err))
		return
	}
	detail, err := h.routePolicies.Set(c.Request.Context(), routepolicy.SetInput{RouteID: request.RouteID, Expression: request.Expression, Description: request.Description, Status: request.Status, ExpectedVersion: request.ExpectedVersion, Permissions: request.Permissions})
	if err != nil {
		Fail(c, h.logger, err)
		return
	}
	OK(c, detail)
}
