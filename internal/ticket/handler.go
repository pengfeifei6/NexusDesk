package ticket

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }
func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	b := g.Group("/organizations/:orgID/tickets")
	b.POST("", h.create)
	b.GET("", h.list)
	b.GET("/:ticketID", h.get)
	b.PATCH("/:ticketID", h.patch)
	b.DELETE("/:ticketID", h.remove)
	b.POST("/:ticketID/transition", h.transition)
	b.POST("/:ticketID/comments", h.comment)
	b.GET("/:ticketID/comments", h.comments)
	b.GET("/:ticketID/events", h.events)
}
func responseError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message, "request_id": ""}})
}
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		c.Header("WWW-Authenticate", "Bearer")
		responseError(c, 401, "UNAUTHENTICATED", "authentication required")
	case errors.Is(err, ErrNotFound):
		responseError(c, 404, "NOT_FOUND", "resource not found")
	case errors.Is(err, ErrForbidden):
		responseError(c, 403, "FORBIDDEN", "permission denied")
	case errors.Is(err, ErrConflict):
		responseError(c, 409, "CONFLICT", "state or version conflict")
	case errors.Is(err, ErrInvalidRequest):
		responseError(c, 400, "INVALID_REQUEST", "invalid request")
	case errors.Is(err, ErrInvalidAssignee):
		responseError(c, 400, "INVALID_ASSIGNEE", "assignee must be active in organization")
	default:
		responseError(c, 500, "INTERNAL_ERROR", "internal server error")
	}
}
func positive(raw string) (int64, error) {
	if raw == "" {
		return 0, ErrInvalidRequest
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, ErrInvalidRequest
		}
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, ErrInvalidRequest
	}
	return n, nil
}
func (h *Handler) scope(c *gin.Context) (actor, org, id int64, ok bool) {
	c.Header("Cache-Control", "no-store")
	actor, ok = auth.CurrentUserID(c)
	if !ok {
		fail(c, ErrUnauthenticated)
		return
	}
	var err error
	org, err = positive(c.Param("orgID"))
	if err != nil {
		fail(c, err)
		return 0, 0, 0, false
	}
	if err = h.service.Authorize(c.Request.Context(), actor, org); err != nil {
		fail(c, err)
		return 0, 0, 0, false
	}
	if raw := c.Param("ticketID"); raw != "" {
		id, err = positive(raw)
		if err != nil {
			fail(c, err)
			return 0, 0, 0, false
		}
	}
	return actor, org, id, true
}
func readFields(c *gin.Context, allowed, required []string) (map[string]json.RawMessage, bool) {
	if c.Request.URL.RawQuery != "" {
		fail(c, ErrInvalidRequest)
		return nil, false
	}
	return auth.ReadJSONFields(c, 64*1024, allowed, required)
}
func text(raw json.RawMessage) (string, error) {
	var s *string
	if json.Unmarshal(raw, &s) != nil || s == nil {
		return "", ErrInvalidRequest
	}
	return *s, nil
}
func integer(raw json.RawMessage, min int64) (int64, error) {
	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || n < min {
		return 0, ErrInvalidRequest
	}
	return n, nil
}
func priority(raw json.RawMessage) (int16, error) {
	n, err := integer(raw, 1)
	if err != nil || n > 3 {
		return 0, ErrInvalidRequest
	}
	return int16(n), nil
}
func publicTicket(t Ticket) gin.H {
	return gin.H{"id": t.ID, "org_id": t.OrgID, "creator_id": t.CreatorID, "assignee_id": t.AssigneeID, "title": t.Title, "body": t.Body, "priority": t.Priority, "status": t.Status, "version": t.Version, "created_at": t.CreatedAt.UTC().Format(time.RFC3339Nano), "updated_at": t.UpdatedAt.UTC().Format(time.RFC3339Nano)}
}
func publicComment(c Comment) gin.H {
	return gin.H{"id": c.ID, "ticket_id": c.TicketID, "author_id": c.AuthorID, "content": c.Content, "created_at": c.CreatedAt.UTC().Format(time.RFC3339Nano)}
}
func publicEvent(e Event) gin.H {
	return gin.H{"id": e.ID, "ticket_id": e.TicketID, "actor_id": e.ActorID, "event_type": e.Type, "ticket_version": e.TicketVersion, "before_data": e.Before, "after_data": e.After, "created_at": e.CreatedAt.UTC().Format(time.RFC3339Nano)}
}
func (h *Handler) create(c *gin.Context) {
	a, o, _, ok := h.scope(c)
	if !ok {
		return
	}
	f, ok := readFields(c, []string{"title", "body", "priority"}, []string{"title", "body"})
	if !ok {
		return
	}
	title, e1 := text(f["title"])
	body, e2 := text(f["body"])
	p := int16(2)
	var e3 error
	if raw, exists := f["priority"]; exists {
		p, e3 = priority(raw)
	}
	if e1 != nil || e2 != nil || e3 != nil {
		fail(c, ErrInvalidRequest)
		return
	}
	t, err := h.service.Create(c.Request.Context(), a, o, CreateInput{Title: title, Body: body, Priority: p})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(201, gin.H{"data": publicTicket(t)})
}
func parseFilter(raw string, filters bool) (Filter, error) {
	f := Filter{Page: Page{1, 20}}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return f, ErrInvalidRequest
	}
	for key, entries := range q {
		if len(entries) != 1 || entries[0] == "" {
			return f, ErrInvalidRequest
		}
		v := entries[0]
		switch key {
		case "page", "page_size":
			n, err := positive(v)
			if err != nil {
				return f, err
			}
			if key == "page" {
				f.Number = n
			} else {
				f.Size = n
			}
		case "status":
			if !filters || !validStatus(Status(v)) {
				return f, ErrInvalidRequest
			}
			f.Status = Status(v)
		case "priority":
			if !filters {
				return f, ErrInvalidRequest
			}
			n, err := positive(v)
			if err != nil || n > 3 {
				return f, ErrInvalidRequest
			}
			p := int16(n)
			f.Priority = &p
		default:
			return f, ErrInvalidRequest
		}
	}
	_, err = f.offset()
	return f, err
}
func (h *Handler) list(c *gin.Context) {
	a, o, _, ok := h.scope(c)
	if !ok {
		return
	}
	if !auth.EmptyGET(c) {
		fail(c, ErrInvalidRequest)
		return
	}
	f, err := parseFilter(c.Request.URL.RawQuery, true)
	if err != nil {
		fail(c, err)
		return
	}
	rows, err := h.service.List(c.Request.Context(), a, o, f)
	if err != nil {
		fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(rows))
	for _, t := range rows {
		items = append(items, publicTicket(t))
	}
	c.JSON(200, gin.H{"data": gin.H{"items": items, "page": f.Number, "page_size": f.Size}})
}
func (h *Handler) get(c *gin.Context) {
	a, o, id, ok := h.scope(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" || !auth.EmptyGET(c) {
		fail(c, ErrInvalidRequest)
		return
	}
	t, err := h.service.Get(c.Request.Context(), a, o, id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": publicTicket(t)})
}
func (h *Handler) patch(c *gin.Context) {
	a, o, id, ok := h.scope(c)
	if !ok {
		return
	}
	f, ok := readFields(c, []string{"expected_version", "title", "body", "priority", "assignee_id"}, []string{"expected_version"})
	if !ok {
		return
	}
	v, err := integer(f["expected_version"], 0)
	if err != nil {
		fail(c, err)
		return
	}
	in := PatchInput{ExpectedVersion: v}
	for _, name := range []string{"title", "body"} {
		if raw, exists := f[name]; exists {
			s, err := text(raw)
			if err != nil {
				fail(c, err)
				return
			}
			if name == "title" {
				in.Title = &s
			} else {
				in.Body = &s
			}
		}
	}
	if raw, exists := f["priority"]; exists {
		p, err := priority(raw)
		if err != nil {
			fail(c, err)
			return
		}
		in.Priority = &p
	}
	if raw, exists := f["assignee_id"]; exists {
		in.AssigneeSet = true
		if string(raw) != "null" {
			n, err := integer(raw, 1)
			if err != nil {
				fail(c, err)
				return
			}
			in.AssigneeID = &n
		}
	}
	t, err := h.service.Patch(c.Request.Context(), a, o, id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": publicTicket(t)})
}
func (h *Handler) remove(c *gin.Context) {
	a, o, id, ok := h.scope(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" || !auth.EmptyGET(c) {
		fail(c, ErrInvalidRequest)
		return
	}
	if err := h.service.Delete(c.Request.Context(), a, o, id); err != nil {
		fail(c, err)
		return
	}
	c.Status(204)
}
func (h *Handler) transition(c *gin.Context) {
	a, o, id, ok := h.scope(c)
	if !ok {
		return
	}
	f, ok := readFields(c, []string{"to_status", "expected_version"}, []string{"to_status", "expected_version"})
	if !ok {
		return
	}
	to, e1 := text(f["to_status"])
	v, e2 := integer(f["expected_version"], 0)
	if e1 != nil || e2 != nil {
		fail(c, ErrInvalidRequest)
		return
	}
	t, err := h.service.Transition(c.Request.Context(), a, o, id, TransitionInput{ExpectedVersion: v, To: Status(to)})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"data": publicTicket(t)})
}
func (h *Handler) comment(c *gin.Context) {
	a, o, id, ok := h.scope(c)
	if !ok {
		return
	}
	f, ok := readFields(c, []string{"content"}, []string{"content"})
	if !ok {
		return
	}
	content, err := text(f["content"])
	if err != nil {
		fail(c, err)
		return
	}
	comment, err := h.service.Comment(c.Request.Context(), a, o, id, content)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(201, gin.H{"data": publicComment(comment)})
}
func (h *Handler) children(c *gin.Context, events bool) {
	a, o, id, ok := h.scope(c)
	if !ok {
		return
	}
	if !auth.EmptyGET(c) {
		fail(c, ErrInvalidRequest)
		return
	}
	f, err := parseFilter(c.Request.URL.RawQuery, false)
	if err != nil {
		fail(c, err)
		return
	}
	items := []gin.H{}
	if events {
		rows, err := h.service.Events(c.Request.Context(), a, o, id, f.Page)
		if err != nil {
			fail(c, err)
			return
		}
		for _, e := range rows {
			items = append(items, publicEvent(e))
		}
	} else {
		rows, err := h.service.Comments(c.Request.Context(), a, o, id, f.Page)
		if err != nil {
			fail(c, err)
			return
		}
		for _, r := range rows {
			items = append(items, publicComment(r))
		}
	}
	c.JSON(200, gin.H{"data": gin.H{"items": items, "page": f.Number, "page_size": f.Size}})
}
func (h *Handler) comments(c *gin.Context) { h.children(c, false) }
func (h *Handler) events(c *gin.Context)   { h.children(c, true) }
