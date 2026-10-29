package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// Headers of a webhook request. The signature is HMAC-SHA256, in hex and prefixed with "sha256=", over the
// timestamp (Unix seconds, decimal), a dot and the request body, keyed with the credential of the channel.
// A receiver that rejects old timestamps is protected against replay.
const (
	HeaderEvent     = "X-Sysmin-Event"
	HeaderTimestamp = "X-Sysmin-Timestamp"
	HeaderSignature = "X-Sysmin-Signature"
)

// maxResponse bounds how much of a response is read.
const maxResponse = 64 << 10

func (n *Notifier) sendWebhook(ctx context.Context, ch config.Channel, c credential, p payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return &deliveryError{msg: "cannot encode the message", code: codeSendFailed}
	}
	h := map[string]string{HeaderEvent: string(p.Event)}
	if c.present {
		ts := strconv.FormatInt(n.now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(c.value))
		mac.Write([]byte(ts + "."))
		mac.Write(body)
		h[HeaderTimestamp] = ts
		h[HeaderSignature] = "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	return n.post(ctx, ch.URL, body, h)
}

func (n *Notifier) sendSlack(ctx context.Context, c credential, p payload) error {
	if err := requireSecure(c.value); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"text": p.text()})
	return n.post(ctx, c.value, body, nil)
}

func (n *Notifier) sendTelegram(ctx context.Context, ch config.Channel, c credential, p payload) error {
	if err := requireSecure(n.opts.telegramURL); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"chat_id": ch.ChatID, "text": p.text()})
	return n.post(ctx, n.opts.telegramURL+"/bot"+url.PathEscape(c.value)+"/sendMessage", body, nil)
}

// requireSecure refuses an address that carries a secret unless it uses https or points to this machine.
func requireSecure(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return &deliveryError{msg: "the address is not a valid URL", code: codeInvalidChannel}
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && isLoopback(u.Hostname()) {
		return nil
	}
	return &deliveryError{msg: "the address must use https", code: codeInvalidChannel}
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// post sends a JSON body. The error text never contains the URL: it may hold a secret.
func (n *Notifier) post(ctx context.Context, target string, body []byte, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return &deliveryError{msg: "the address is not a valid URL", code: codeInvalidChannel}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "sysmin")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := n.opts.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return &deliveryError{msg: "request failed: " + err.Error(), code: codeSendFailed, retry: true}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	retry := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode == http.StatusRequestTimeout
	return &deliveryError{msg: fmt.Sprintf("unexpected status %d", resp.StatusCode), code: codeSendFailed, retry: retry}
}
