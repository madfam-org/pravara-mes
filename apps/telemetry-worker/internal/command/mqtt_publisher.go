package command

import (
	"context"
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// PahoPublisher publishes commands through a paho MQTT client at QoS 1,
// non-retained, and waits for the broker's PUBACK.
type PahoPublisher struct {
	client  mqtt.Client
	timeout time.Duration
}

// NewPahoPublisher wraps a connected paho client.
func NewPahoPublisher(client mqtt.Client) *PahoPublisher {
	return &PahoPublisher{client: client, timeout: 10 * time.Second}
}

// Publish implements MessagePublisher.
func (p *PahoPublisher) Publish(ctx context.Context, topic string, payload []byte) error {
	if !p.client.IsConnectionOpen() {
		return fmt.Errorf("mqtt client not connected")
	}
	token := p.client.Publish(topic, 1, false, payload)

	timer := time.NewTimer(p.timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("mqtt publish timeout after %s", p.timeout)
	case <-token.Done():
		if err := token.Error(); err != nil {
			return fmt.Errorf("mqtt publish failed: %w", err)
		}
	}
	return nil
}
