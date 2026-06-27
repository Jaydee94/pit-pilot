package push

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// WebPusher sends push notifications via the Web Push Protocol using VAPID auth.
type WebPusher struct {
	publicKey  string
	privateKey string
	subject    string
	client     *http.Client
}

// NewWebPusher returns a WebPusher configured with the given VAPID keys and subject.
func NewWebPusher(publicKey, privateKey, subject string) *WebPusher {
	return &WebPusher{
		publicKey:  publicKey,
		privateKey: privateKey,
		subject:    subject,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Send encrypts the payload as JSON and delivers it to the given subscription.
// Returns dead=true when the push service responds with 404 or 410, indicating
// the subscription is no longer valid and should be deleted.
func (w *WebPusher) Send(ctx context.Context, sub Subscription, p Payload) (bool, error) {
	body, err := json.Marshal(map[string]string{
		"title": p.Title,
		"body":  p.Body,
		"url":   p.URL,
	})
	if err != nil {
		return false, err
	}

	resp, err := webpush.SendNotificationWithContext(ctx, body, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256dh,
			Auth:   sub.Auth,
		},
	}, &webpush.Options{
		HTTPClient:      w.client,
		Subscriber:      w.subject,
		VAPIDPublicKey:  w.publicKey,
		VAPIDPrivateKey: w.privateKey,
		TTL:             86400,
	})
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 || resp.StatusCode == 410 {
		return true, nil
	}
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("push failed: status %d", resp.StatusCode)
	}
	return false, nil
}
