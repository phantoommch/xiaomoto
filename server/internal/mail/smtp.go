package mail

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type Config struct {
	Host string
	Port string
	User string
	Pass string
	From string
	TLS  string
}

func (c Config) Enabled() bool {
	return strings.TrimSpace(c.Host) != "" && strings.TrimSpace(c.From) != ""
}

func (c Config) port() string {
	if p := strings.TrimSpace(c.Port); p != "" {
		return p
	}
	if c.mode() == "ssl" {
		return "465"
	}
	return "587"
}

func (c Config) mode() string {
	switch strings.ToLower(strings.TrimSpace(c.TLS)) {
	case "ssl", "smtps", "implicit":
		return "ssl"
	case "none", "off", "plain":
		return "none"
	case "starttls", "tls":
		return "starttls"
	default:
		if strings.TrimSpace(c.Port) == "465" {
			return "ssl"
		}
		return "starttls"
	}
}

// FromName is the display name shown in the From header.
const FromName = "小moto安全驾驶指南"

func (c Config) Send(to, subject, body string) error {
	if !c.Enabled() {
		return fmt.Errorf("smtp not configured")
	}
	to = strings.TrimSpace(to)
	if strings.ContainsAny(to, "\r\n<>") {
		return fmt.Errorf("invalid recipient")
	}
	host := strings.TrimSpace(c.Host)
	addr := net.JoinHostPort(host, c.port())
	msg, err := c.buildMessage(to, subject, body, time.Now())
	if err != nil {
		return err
	}
	var client *smtp.Client
	if c.mode() == "ssl" {
		conn, dialErr := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
		if dialErr != nil {
			return dialErr
		}
		client, err = smtp.NewClient(conn, host)
	} else {
		client, err = smtp.Dial(addr)
		if err == nil && c.mode() == "starttls" {
			err = client.StartTLS(&tls.Config{ServerName: host})
		}
	}
	if err != nil {
		return err
	}
	defer client.Close()
	if strings.TrimSpace(c.User) != "" {
		if err := client.Auth(smtp.PlainAuth("", c.User, c.Pass, host)); err != nil {
			return err
		}
	}
	if err := client.Mail(strings.TrimSpace(c.From)); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(msg); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// buildMessage renders an RFC 5322 message with a UTF-8 subject and display
// name (RFC 2047 encoded words) and a base64 text/plain UTF-8 body.
func (c Config) buildMessage(to, subject, body string, now time.Time) ([]byte, error) {
	from := strings.TrimSpace(c.From)
	msgID, err := messageID(from, now)
	if err != nil {
		return nil, err
	}
	fromHeader := mime.BEncoding.Encode("UTF-8", FromName) + " <" + from + ">"
	text := strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	var b strings.Builder
	b.WriteString("Date: " + now.Format(time.RFC1123Z) + "\r\n")
	b.WriteString("Message-ID: " + msgID + "\r\n")
	b.WriteString("From: " + fromHeader + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mime.BEncoding.Encode("UTF-8", subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	b.WriteString("\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	if encoded != "" {
		b.WriteString(encoded + "\r\n")
	}
	return []byte(b.String()), nil
}

func messageID(from string, now time.Time) (string, error) {
	domain := "example.com"
	if at := strings.LastIndex(from, "@"); at >= 0 && at < len(from)-1 {
		domain = from[at+1:]
	}
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("<%d.%s@%s>", now.UnixNano(), hex.EncodeToString(buf), domain), nil
}

// MaskEmail keeps the first character of the local part and the domain,
// e.g. user@example.com -> u***@example.com.
func MaskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return "***"
	}
	return email[:1] + "***" + email[at:]
}

func ActivationBody(code string) string {
	return "你的激活码是 " + code + "。\n10 分钟内有效。\n如果不是你本人操作，请忽略这封邮件。\n"
}
