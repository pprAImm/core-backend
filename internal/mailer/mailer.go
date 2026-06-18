package mailer

import (
	"fmt"
	"net/smtp"
	"os"
)

type Mailer struct {
	From     string
	Host     string
	Port     string
	Username string
	Password string
}

func NewFromEnv() *Mailer {
	return &Mailer{
		From:     os.Getenv("SMTP_FROM"),
		Host:     os.Getenv("SMTP_HOST"),
		Port:     os.Getenv("SMTP_PORT"),
		Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"),
	}
}

func (m *Mailer) IsConfigured() bool {
	return m.Host != "" && m.Port != "" && m.From != ""
}

func (m *Mailer) Send(to, subject, body string) error {
	addr := fmt.Sprintf("%s:%s", m.Host, m.Port)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		m.From, to, subject, body)

	var auth smtp.Auth
	if m.Username != "" {
		auth = smtp.PlainAuth("", m.Username, m.Password, m.Host)
	}

	return smtp.SendMail(addr, auth, m.From, []string{to}, []byte(msg))
}

func (m *Mailer) SendVerificationEmail(to, username, token, baseURL string) error {
	link := fmt.Sprintf("%s/verify?token=%s", baseURL, token)
	subject := "Подтверждение email – prAIm"
	body := fmt.Sprintf(`Здравствуйте, %s!

Благодарим за регистрацию на платформе prAIm.

Для подтверждения вашего email перейдите по ссылке:
%s

Если вы не регистрировались на prAIm, просто проигнорируйте это письмо.

С уважением,
Команда prAIm`, username, link)

	return m.Send(to, subject, body)
}
