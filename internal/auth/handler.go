package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
)

const maxAuthBody = 16 * 1024

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/auth/register", h.register)
}

func apiError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message, "request_id": ""}})
}

// ReadStringFields reuses the strict bounded JSON contract for public and protected writes.
func ReadStringFields(c *gin.Context, names ...string) (map[string]string, bool) {
	body, ok := readAuthBody(c)
	if !ok {
		return nil, false
	}
	fields, err := decodeAuthFields(body, names...)
	if err != nil {
		apiError(c, 400, "INVALID_REQUEST", "invalid request")
		return nil, false
	}
	return fields, true
}

// ReadObjectFields provides the same bounded, exact-key JSON contract for typed fields.
func ReadObjectFields(c *gin.Context, names ...string) (map[string]json.RawMessage, bool) {
	body, ok := readAuthBody(c)
	if !ok {
		return nil, false
	}
	values, err := decodeObjectFields(body, names...)
	if err != nil {
		apiError(c, 400, "INVALID_REQUEST", "invalid request")
		return nil, false
	}
	return values, true
}

func decodeObjectFields(body []byte, fields ...string) (map[string]json.RawMessage, error) {
	if !validJSONUnicode(body) {
		return nil, ErrInvalidRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, ErrInvalidRequest
	}
	values := make(map[string]json.RawMessage, len(fields))
	for decoder.More() {
		keyToken, err := decoder.Token()
		key, ok := keyToken.(string)
		allowed := false
		for _, field := range fields {
			allowed = allowed || key == field
		}
		if err != nil || !ok || !allowed {
			return nil, ErrInvalidRequest
		}
		if _, exists := values[key]; exists {
			return nil, ErrInvalidRequest
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, ErrInvalidRequest
		}
		values[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || len(values) != len(fields) {
		return nil, ErrInvalidRequest
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrInvalidRequest
	}
	return values, nil
}

func decodeAuthFields(body []byte, fields ...string) (map[string]string, error) {
	raw, err := decodeObjectFields(body, fields...)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(raw))
	for name, data := range raw {
		var value *string
		if json.Unmarshal(data, &value) != nil || value == nil || *value == "" {
			return nil, ErrInvalidRequest
		}
		values[name] = *value
	}
	return values, nil
}

func decodeRegister(body []byte) (RegisterInput, error) {
	values, err := decodeAuthFields(body, "email", "display_name", "password")
	if err != nil {
		return RegisterInput{}, err
	}
	return RegisterInput{Email: values["email"], DisplayName: values["display_name"], Password: values["password"]}, nil
}

func readAuthBody(c *gin.Context) ([]byte, bool) {
	c.Header("Cache-Control", "no-store")
	if c.Request.ContentLength > maxAuthBody {
		apiError(c, 413, "REQUEST_TOO_LARGE", "request body too large")
		return nil, false
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apiError(c, 415, "UNSUPPORTED_MEDIA_TYPE", "application/json required")
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxAuthBody+1))
	if len(body) > maxAuthBody {
		apiError(c, 413, "REQUEST_TOO_LARGE", "request body too large")
		return nil, false
	}
	if err != nil {
		apiError(c, 400, "INVALID_REQUEST", "invalid request")
		return nil, false
	}
	return body, true
}

func writeAuthFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidRequest), errors.Is(err, ErrInvalidArgument):
		apiError(c, 400, "INVALID_REQUEST", "invalid request")
	case errors.Is(err, ErrInvalidEmail):
		apiError(c, 400, "INVALID_EMAIL", "invalid email")
	case errors.Is(err, ErrInvalidPassword):
		apiError(c, 400, "INVALID_PASSWORD", "invalid password")
	case errors.Is(err, ErrEmailExists):
		apiError(c, 409, "EMAIL_ALREADY_EXISTS", "email already registered")
	case errors.Is(err, ErrAuthBusy):
		apiError(c, 503, "AUTH_BUSY", "authentication service busy")
	case errors.Is(err, ErrInvalidCredentials):
		apiError(c, 401, "INVALID_CREDENTIALS", "invalid email or password")
	default:
		apiError(c, 500, "INTERNAL_ERROR", "internal server error")
	}
}

func (h *Handler) register(c *gin.Context) {
	body, ok := readAuthBody(c)
	if !ok {
		return
	}
	input, err := decodeRegister(body)
	if err != nil {
		apiError(c, 400, "INVALID_REQUEST", "invalid request")
		return
	}
	user, err := h.service.Register(c.Request.Context(), input)
	if err != nil {
		writeAuthFailure(c, err)
		return
	}
	// Explicit response DTO: adding fields to a persistence model cannot leak them.
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{
		"id": user.ID, "email": user.Email, "display_name": user.DisplayName,
	}})
}

type LoginHandler struct{ service *LoginService }

func NewLoginHandler(service *LoginService) *LoginHandler { return &LoginHandler{service: service} }

func (h *LoginHandler) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/auth/login", h.login)
}

func (h *LoginHandler) login(c *gin.Context) {
	body, ok := readAuthBody(c)
	if !ok {
		return
	}
	fields, err := decodeAuthFields(body, "email", "password")
	if err != nil {
		apiError(c, 400, "INVALID_REQUEST", "invalid request")
		return
	}
	result, err := h.service.Login(c.Request.Context(), LoginInput{Email: fields["email"], Password: fields["password"]})
	if err != nil {
		writeAuthFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}
