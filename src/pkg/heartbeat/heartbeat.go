// Package heartbeat drives the Finance-FE productivity cron endpoints on a
// timer. The Next.js app has no always-on runtime, so this 24/7 gateway
// process POSTs its reminder and daily-digest endpoints on a schedule.
package heartbeat

import (
	"context"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

type Config struct {
	RemindersURL string
	DigestURL    string
	Secret       string
	Interval     time.Duration
	Enabled      bool
}

// Start is non-blocking: it spawns a goroutine that ticks every cfg.Interval,
// POSTing the reminders endpoint every tick and the digest endpoint every 15th
// tick. It returns immediately. A disabled config is a no-op.
func Start(ctx context.Context, cfg Config) {
	if !cfg.Enabled {
		return
	}
	go func() {
		client := &http.Client{Timeout: 30 * time.Second}
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		n := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n++
				post(ctx, client, cfg.RemindersURL, cfg.Secret)
				if n%15 == 0 {
					post(ctx, client, cfg.DigestURL, cfg.Secret)
				}
			}
		}
	}()
}

func post(ctx context.Context, client *http.Client, url, secret string) {
	if url == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		logrus.Warnf("heartbeat: build request for %s: %v", url, err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := client.Do(req)
	if err != nil {
		logrus.Warnf("heartbeat: POST %s: %v", url, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		logrus.Warnf("heartbeat: POST %s -> %d", url, resp.StatusCode)
		return
	}
	logrus.Debugf("heartbeat: POST %s -> %d", url, resp.StatusCode)
}
