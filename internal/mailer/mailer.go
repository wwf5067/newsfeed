// Package mailer 共享 SMTP 发送逻辑。
//
// 历史:digest 包里曾内嵌一份 SMTP 发送代码,subscribe 包要发邮件时
// 复制一份显然不合理。抽到这里,让 digest / subscribe 共用一套
// 处理 QQ 邮箱的"显式 TLS + base64 中文 Subject"细节。
package mailer

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"time"
)

// Config SMTP 凭据集合。
type Config struct {
	Host string
	Port int
	User string
	Pass string
	From string // 发件人地址,通常等于 User
	To   string // 收件人(单值;多收件人后续按需扩展)
}

// Valid 判断配置是否齐全,缺关键项直接跳过发送(让上层优雅降级而不是报错)。
func (c Config) Valid() bool {
	return c.Host != "" && c.User != "" && c.Pass != "" && c.From != "" && c.To != ""
}

// Send 通过 SMTPS(隐式 TLS,通常 465 端口)发一封 HTML 邮件。
//
// 没用 net/smtp.SendMail:它默认走 STARTTLS,QQ 邮箱要求一上来就 TLS,
// 这里直接 tls.Dial → smtp.NewClient 走显式流程。
func Send(cfg Config, subject, htmlBody string) error {
	if !cfg.Valid() {
		return fmt.Errorf("smtp config incomplete")
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	tlsCfg := &tls.Config{ServerName: cfg.Host}

	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	defer conn.Close()

	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp new client: %w", err)
	}
	defer c.Quit()

	auth := smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := c.Mail(cfg.From); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := c.Rcpt(cfg.To); err != nil {
		return fmt.Errorf("rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	defer w.Close()

	msg := buildMessage(cfg.From, cfg.To, subject, htmlBody)
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	return nil
}

// buildMessage 拼一封 RFC5322 + MIME HTML 邮件。
// 中文 Subject 用 =?UTF-8?B?...?= base64 编码,保证客户端正确显示。
func buildMessage(from, to, subject, htmlBody string) string {
	encSubject := mimeEncode(subject)
	headers := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + encSubject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"Date: " + time.Now().Format(time.RFC1123Z),
	}
	var b bytes.Buffer
	for _, h := range headers {
		b.WriteString(h)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	b.WriteString(htmlBody)
	return b.String()
}

// mimeEncode 把任意 string 编码成 RFC2047 形式,保证非 ASCII 在邮件 header 里正确显示。
// 全 ASCII 直接返回避免无意义编码;含非 ASCII 时走 =?UTF-8?B?...?= base64。
func mimeEncode(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
		}
	}
	return s
}
