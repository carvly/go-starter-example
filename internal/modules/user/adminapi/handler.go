// Package adminapi 是 user 的后台 HTTP 适配器：Bind → Validate → 调 Service →
// 序列化，不含业务规则。
package adminapi

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"app.local/internal/kit/page"
	"app.local/internal/modules/user/adminapi/oapi"
	"app.local/internal/modules/user/domain"
	"app.local/internal/platform/httpserver"
)

type Handler struct {
	service *domain.Service
}

func NewHandler(service *domain.Service) *Handler {
	return &Handler{service: service}
}

func toUser(v *domain.User) oapi.User {
	return oapi.User{
		Id:           v.ID,
		DisplayName:  v.DisplayName,
		AvatarUrl:    v.AvatarUrl,
		Status:       oapi.UserStatus(v.Status),
		LastLoginAt:  v.LastLoginAt,
		SignupClient: oapi.UserSessionClient(v.SignupClient),
		CreatedAt:    v.CreatedAt,
		UpdatedAt:    v.UpdatedAt,
	}
}

func (h *Handler) ListUsers(c *echo.Context, params oapi.ListUsersParams) error {
	p, err := httpserver.PageRequest(params.Page, params.Size)
	if err != nil {
		return err
	}
	sort := ""
	if params.Sort != nil {
		sort = string(*params.Sort)
	}
	s, err := page.ParseSort(sort, domain.DefaultSort, domain.SortFields...)
	if err != nil {
		return err
	}
	var f domain.UserFilter
	if params.Q != nil {
		f.Keyword = *params.Q
	}
	if params.Status != nil {
		for _, v := range *params.Status {
			f.Status = append(f.Status, domain.UserStatus(v))
		}
	}
	f.CreatedAtFrom, f.CreatedAtTo = params.CreatedAtFrom, params.CreatedAtTo
	f.LastLoginAtFrom, f.LastLoginAtTo = params.LastLoginAtFrom, params.LastLoginAtTo
	r, err := h.service.List(c.Request().Context(), f, s, p)
	if err != nil {
		return err
	}
	resp := oapi.UserPage{Items: make([]oapi.User, 0, len(r.Items)), Total: r.Total, TotalCapped: r.TotalCapped}
	for _, v := range r.Items {
		resp.Items = append(resp.Items, toUser(v))
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *Handler) GetUser(c *echo.Context, id oapi.ID) error {
	v, err := h.service.Get(c.Request().Context(), id.String())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toUser(v))
}

func (h *Handler) DisableUser(c *echo.Context, id oapi.ID) error {
	v, err := h.service.Disable(c.Request().Context(), id.String())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toUser(v))
}

func (h *Handler) EnableUser(c *echo.Context, id oapi.ID) error {
	v, err := h.service.Enable(c.Request().Context(), id.String())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, toUser(v))
}

// ListUserIdentities 是后台的用户详情页：该用户的登录方式（只读）。
func (h *Handler) ListUserIdentities(c *echo.Context, id oapi.ID) error {
	idns, err := h.service.ListIdentities(c.Request().Context(), id.String())
	if err != nil {
		return err
	}
	resp := oapi.UserIdentityList{Items: make([]oapi.UserIdentity, 0, len(idns))}
	for _, v := range idns {
		resp.Items = append(resp.Items, oapi.UserIdentity{
			Id:          v.ID,
			Provider:    oapi.IdentityProvider(v.Provider),
			Subject:     &v.Subject,
			LastLoginAt: v.LastLoginAt,
			CreatedAt:   v.CreatedAt,
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// ListUserSessions 是后台的用户详情页：该用户的登录会话（不返回 refresh_family
// 和任何 token 摘要；已吊销的会话在列表里消失）。
func (h *Handler) ListUserSessions(c *echo.Context, id oapi.ID) error {
	sessions, err := h.service.ListSessions(c.Request().Context(), id.String())
	if err != nil {
		return err
	}
	resp := oapi.UserSessionList{Items: make([]oapi.UserSession, 0, len(sessions))}
	for _, v := range sessions {
		resp.Items = append(resp.Items, oapi.UserSession{
			Id:         v.ID,
			Client:     oapi.UserSessionClient(v.Client),
			DeviceName: v.DeviceName,
			Ip:         v.IP,
			UserAgent:  v.UserAgent,
			CreatedAt:  v.CreatedAt,
			LastUsedAt: v.LastUsedAt,
			ExpiresAt:  v.ExpiresAt,
		})
	}
	return c.JSON(http.StatusOK, resp)
}

// KickUserSession 是后台踢下线：只吊销属于该用户的会话。sid 的类型和 ID 相同
// （openapi_types.UUID 的别名）。
func (h *Handler) KickUserSession(c *echo.Context, id oapi.ID, sid oapi.ID) error {
	if err := h.service.KickSession(c.Request().Context(), id.String(), sid.String()); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

var _ oapi.ServerInterface = (*Handler)(nil)
