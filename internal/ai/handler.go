package ai

import (
	"context"
	"encoding/json"
	"github.com/pengfeifei6/NexusDesk/internal/platform/observe"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pengfeifei6/NexusDesk/internal/auth"
)

type MessageLimiter interface {
	Check(context.Context, int64, int64) (bool, time.Duration, error)
}

type Handler struct {
	service *ChatService
	limiter MessageLimiter
}

func (h *Handler) WithLimiter(l MessageLimiter) *Handler { h.limiter = l; return h }

func NewHandler(service *ChatService) *Handler { return &Handler{service: service} }
func (h *Handler) RegisterRoutes(g *gin.RouterGroup) {
	b := g.Group("/organizations/:orgID/ai")
	b.POST("/conversations", h.create)
	b.GET("/conversations", h.list)
	b.GET("/conversations/:conversationID/messages", h.history)
	b.POST("/conversations/:conversationID/messages", h.send)
	b.POST("/tool-actions/:actionID/confirm", h.confirm)
	b.POST("/tool-actions/:actionID/reject", h.reject)
}
func httpStatus(code string) int {
	switch code {
	case "INVALID_REQUEST":
		return 400
	case "UNAUTHENTICATED":
		return 401
	case "FORBIDDEN":
		return 403
	case "NOT_FOUND":
		return 404
	case "AI_CONFLICT":
		return 409
	case "AI_EXPIRED":
		return 410
	case "AI_BUSY", "RATE_LIMITED":
		return 429
	case "AI_LIMIT":
		return 422
	case "MODEL_INVALID_RESPONSE":
		return 502
	case "MODEL_UNAVAILABLE", "RATE_LIMIT_UNAVAILABLE":
		return 503
	case "MODEL_TIMEOUT":
		return 504
	}
	return 500
}
func errorPayload(err error) gin.H {
	code := Code(publicError(err))
	return gin.H{"code": code, "message": code, "retryable": code == "AI_BUSY" || code == "MODEL_UNAVAILABLE" || code == "MODEL_TIMEOUT"}
}
func failure(c *gin.Context, err error) {
	code := Code(publicError(err))
	if code == "UNAUTHENTICATED" {
		c.Header("WWW-Authenticate", "Bearer")
	}
	c.JSON(httpStatus(code), gin.H{"error": gin.H{"code": code, "message": code, "request_id": observe.RequestID(c.Request.Context())}})
}
func (h *Handler) scope(c *gin.Context) (Actor, bool) {
	c.Header("Cache-Control", "no-store")
	id, ok := auth.CurrentUserID(c)
	if !ok {
		failure(c, NewError("UNAUTHENTICATED"))
		return Actor{}, false
	}
	raw := c.Param("orgID")
	org, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || org <= 0 || strconv.FormatInt(org, 10) != raw {
		failure(c, NewError("INVALID_REQUEST"))
		return Actor{}, false
	}
	a := Actor{UserID: id, OrgID: org}
	if err = h.service.store.Authorize(c.Request.Context(), a); err != nil {
		failure(c, err)
		return a, false
	}
	if c.Request.URL.RawQuery != "" {
		failure(c, NewError("INVALID_REQUEST"))
		return a, false
	}
	return a, true
}
func noInput(c *gin.Context) bool {
	if !auth.EmptyGET(c) {
		failure(c, NewError("INVALID_REQUEST"))
		return false
	}
	return true
}
func stringInput(c *gin.Context, names ...string) (map[string]string, bool) {
	fields, ok := auth.ReadJSONFields(c, 32*1024, names, names)
	if !ok {
		return nil, false
	}
	result := map[string]string{}
	for _, name := range names {
		var value *string
		if json.Unmarshal(fields[name], &value) != nil || value == nil {
			failure(c, NewError("INVALID_REQUEST"))
			return nil, false
		}
		result[name] = *value
	}
	return result, true
}
func (h *Handler) create(c *gin.Context) {
	a, ok := h.scope(c)
	if !ok {
		return
	}
	v, ok := stringInput(c, "title")
	if !ok {
		return
	}
	out, err := h.service.store.CreateConversation(c.Request.Context(), a, v["title"])
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(201, gin.H{"data": out})
}
func (h *Handler) list(c *gin.Context) {
	a, ok := h.scope(c)
	if !ok || !noInput(c) {
		return
	}
	out, err := h.service.store.ListConversations(c.Request.Context(), a)
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": out})
}
func (h *Handler) history(c *gin.Context) {
	a, ok := h.scope(c)
	if !ok || !noInput(c) {
		return
	}
	out, err := h.service.store.History(c.Request.Context(), a, c.Param("conversationID"))
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": out})
}
func (h *Handler) confirm(c *gin.Context) { h.decide(c, true) }
func (h *Handler) reject(c *gin.Context)  { h.decide(c, false) }
func (h *Handler) decide(c *gin.Context, confirm bool) {
	a, ok := h.scope(c)
	if !ok || !noInput(c) {
		return
	}
	var out Action
	var err error
	if confirm {
		out, err = h.service.store.Confirm(c.Request.Context(), a, c.Param("actionID"))
	} else {
		out, err = h.service.store.Reject(c.Request.Context(), a, c.Param("actionID"))
	}
	if err != nil {
		failure(c, err)
		return
	}
	c.JSON(200, gin.H{"data": out})
}

func (h *Handler) send(c *gin.Context) {
	a, ok := h.scope(c)
	if !ok {
		return
	}
	input, ok := stringInput(c, "content", "client_request_id")
	if !ok {
		return
	}
	if err := h.service.store.PreflightTurn(c.Request.Context(), a, c.Param("conversationID"), input["client_request_id"], input["content"]); err != nil {
		failure(c, err)
		return
	}
	if h.limiter != nil {
		allowed, retry, err := h.limiter.Check(c.Request.Context(), a.OrgID, a.UserID)
		if err != nil {
			c.Header("Retry-After", "1")
			failure(c, NewError("RATE_LIMIT_UNAVAILABLE"))
			return
		}
		if !allowed {
			seconds := int64(retry / time.Second)
			if retry%time.Second != 0 {
				seconds++
			}
			if seconds < 1 {
				seconds = 1
			}
			c.Header("Retry-After", strconv.FormatInt(seconds, 10))
			failure(c, NewError("RATE_LIMITED"))
			return
		}
	}
	sse, err := newSSE(c.Writer)
	if err != nil {
		failure(c, err)
		return
	}
	release, err := h.service.acquire()
	if err != nil {
		failure(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	turn, err := h.service.store.StartTurn(ctx, a, c.Param("conversationID"), input["client_request_id"], input["content"], h.service.runner.model.Model())
	if err != nil {
		release()
		failure(c, err)
		return
	}
	type event struct {
		kind  string
		value any
	}
	type outcome struct {
		result RunResult
		err    error
	}
	events := make(chan event)
	done := make(chan outcome, 1)
	// One producer, one HTTP writer. The provider request is tied to ctx; a
	// disconnect unblocks sends and runs bounded independent database cleanup.
	go func() {
		defer release()
		result, err := h.service.generate(ctx, a, turn, func(kind string, value any) error {
			select {
			case events <- event{kind, value}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		done <- outcome{result, err}
	}()
	if err = sse.Send("message_start", gin.H{"message_id": turn.Assistant.ID, "user_message_id": turn.User.ID, "conversation_id": turn.Assistant.ConversationID, "client_request_id": turn.User.ClientRequestID}); err != nil {
		return
	}
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case event := <-events:
			if err := sse.Send(event.kind, event.value); err != nil {
				return
			}
		case out := <-done:
			if out.err != nil {
				_ = sse.Send("error", errorPayload(out.err))
				return
			}
			_ = sse.Send("message_end", gin.H{"message_id": turn.Assistant.ID, "status": "COMPLETED", "usage": out.result.Usage})
			return
		case <-heartbeat.C:
			if err := sse.Ping(); err != nil {
				return
			}
		case <-ctx.Done():
			if c.Request.Context().Err() == nil {
				_ = sse.Send("error", errorPayload(NewError("MODEL_TIMEOUT")))
			}
			return
		}
	}
}
