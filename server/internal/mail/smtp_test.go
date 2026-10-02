package mail

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	netmail "net/mail"
	"strings"
	"testing"
	"time"
)

func TestBuildMessageHeadersAndUTF8(t *testing.T) {
	c := Config{Host: "smtp.example.com", From: "you@example.com"}
	now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.FixedZone("CST", 8*3600))
	raw, err := c.buildMessage("rider@example.com", "小moto安全驾驶指南 激活码", ActivationBody("123456"), now)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(raw, []byte("\n")) != bytes.Count(raw, []byte("\r\n")) {
		t.Fatal("bare LF in message")
	}
	msg, err := netmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	h := msg.Header
	if _, err := h.Date(); err != nil {
		t.Fatalf("date %q: %v", h.Get("Date"), err)
	}
	if h.Get("Date") != "Fri, 02 Oct 2026 14:00:00 +0800" {
		t.Fatalf("date %q", h.Get("Date"))
	}
	id := h.Get("Message-ID")
	if !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@example.com>") {
		t.Fatalf("message-id %q", id)
	}
	from, err := h.AddressList("From")
	if err != nil || len(from) != 1 || from[0].Name != FromName || from[0].Address != "you@example.com" {
		t.Fatalf("from %q %v %v", h.Get("From"), from, err)
	}
	if strings.ContainsAny(h.Get("From"), "小") {
		t.Fatalf("from not encoded %q", h.Get("From"))
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(h.Get("Subject"))
	if err != nil || subject != "小moto安全驾驶指南 激活码" {
		t.Fatalf("subject %q %v", subject, err)
	}
	if h.Get("Content-Type") != "text/plain; charset=UTF-8" || h.Get("Content-Transfer-Encoding") != "base64" || h.Get("MIME-Version") != "1.0" {
		t.Fatalf("mime headers %v", h)
	}
	body, _ := io.ReadAll(msg.Body)
	for _, line := range strings.Split(strings.TrimRight(string(body), "\r\n"), "\r\n") {
		if len(line) > 76 {
			t.Fatalf("body line too long %d", len(line))
		}
	}
	decoded, err := decodeB64(string(body))
	if err != nil || !strings.Contains(decoded, "你的激活码是 123456。") || !strings.Contains(decoded, "\r\n") {
		t.Fatalf("body %q %v", decoded, err)
	}

	other, _ := c.buildMessage("rider@example.com", "s", "b", now)
	otherMsg, _ := netmail.ReadMessage(bytes.NewReader(other))
	if otherMsg.Header.Get("Message-ID") == id {
		t.Fatal("message-id reused")
	}
}

func TestSendRejectsHeaderInjection(t *testing.T) {
	c := Config{Host: "127.0.0.1", Port: "1", From: "you@example.com", TLS: "none"}
	if err := c.Send("a@example.com\r\nBcc: x@example.com", "s", "b"); err == nil || err.Error() != "invalid recipient" {
		t.Fatalf("expected invalid recipient, got %v", err)
	}
}

func TestMaskEmail(t *testing.T) {
	cases := map[string]string{
		"user@example.com": "u***@example.com",
		"a@b.co":           "a***@b.co",
		"broken":           "***",
	}
	for in, want := range cases {
		if got := MaskEmail(in); got != want {
			t.Fatalf("MaskEmail(%q)=%q want %q", in, got, want)
		}
	}
}

func decodeB64(s string) (string, error) {
	out, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\r", "", "\n", "").Replace(s))
	return string(out), err
}
