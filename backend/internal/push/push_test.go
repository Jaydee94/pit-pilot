package push_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jaydee94/pit-pilot/backend/internal/push"
)

func TestFakePusherRecordsAndReportsDead(t *testing.T) {
	var p push.Pusher = &push.FakePusher{Dead: true}
	dead, err := p.Send(context.Background(), push.Subscription{Endpoint: "e"}, push.Payload{Title: "T"})
	if err != nil || !dead {
		t.Fatalf("expected dead=true err=nil, got dead=%v err=%v", dead, err)
	}
}

func TestFakePusherReturnsErr(t *testing.T) {
	f := &push.FakePusher{Err: errors.New("boom")}
	if _, err := f.Send(context.Background(), push.Subscription{}, push.Payload{}); err == nil {
		t.Fatal("expected error")
	}
	if len(f.Sent) != 1 {
		t.Fatalf("expected 1 recorded send, got %d", len(f.Sent))
	}
}
