package push

import "context"

// Subscription holds the push endpoint and encryption keys for a browser subscription.
type Subscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

// Payload is the notification content to deliver.
type Payload struct {
	Title string
	Body  string
	URL   string
}

// Pusher sends a web push notification.
// dead==true means the subscription is gone (HTTP 404/410) and should be deleted.
type Pusher interface {
	Send(ctx context.Context, sub Subscription, p Payload) (dead bool, err error)
}

// SentRecord captures a single call to FakePusher.Send.
type SentRecord struct {
	Sub     Subscription
	Payload Payload
}

// FakePusher is a test double that records every Send call and returns the
// configured Dead/Err values.
type FakePusher struct {
	Dead bool
	Err  error
	Sent []SentRecord
}

func (f *FakePusher) Send(_ context.Context, sub Subscription, p Payload) (bool, error) {
	f.Sent = append(f.Sent, SentRecord{Sub: sub, Payload: p})
	return f.Dead, f.Err
}
