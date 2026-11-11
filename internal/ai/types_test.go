package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeArguments(t *testing.T) {
	got, err := NormalizeArguments(TicketArguments{Title: "  测试  ", Body: " body \n", Priority: 2})
	if err != nil || got.Title != "测试" || got.Body != "body" {
		t.Fatalf("normalization: %#v %v", got, err)
	}
	for _, in := range []TicketArguments{{Title: "", Body: "body", Priority: 2}, {Title: "x", Body: " ", Priority: 2}, {Title: "x\x00", Body: "body", Priority: 2}, {Title: "x", Body: string([]byte{255}), Priority: 2}, {Title: strings.Repeat("界", 201), Body: "body", Priority: 2}, {Title: "x", Body: strings.Repeat("界", 10001), Priority: 2}, {Title: "x", Body: "body", Priority: 0}, {Title: "x", Body: "body", Priority: 4}} {
		if _, err := NormalizeArguments(in); Code(err) != "INVALID_REQUEST" {
			t.Fatalf("accepted %#v: %v", in, err)
		}
	}
}
func TestUUIDAndSafeErrors(t *testing.T) {
	if !ValidUUID("550e8400-e29b-41d4-a716-446655440000") {
		t.Fatal("canonical UUID rejected")
	}
	for _, v := range []string{"550E8400-e29b-41d4-a716-446655440000", "550e8400e29b41d4a716446655440000", "550e8400-e29b-41d4-a716-44665544000z", ""} {
		if ValidUUID(v) {
			t.Fatal("accepted", v)
		}
	}
	if Code(errors.New("secret SQL")) != "INTERNAL_ERROR" || Code(NewError("secret")) != "INTERNAL_ERROR" || Code(NewError("AI_CONFLICT")) != "AI_CONFLICT" {
		t.Fatal("unsafe error mapping")
	}
	if !errors.Is(NewError("AI_BUSY"), NewError("AI_BUSY")) {
		t.Fatal("code identity")
	}
	if Code(context.Canceled) != "INTERNAL_ERROR" {
		t.Fatal("context must be handled separately")
	}
}
