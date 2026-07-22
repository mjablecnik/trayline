package main

import (
	"fmt"
	"net/smtp"
)

// Notifier sends a notification when a Task fails (Requirement 4.3).
type Notifier interface {
	NotifyFailure(task *Task) error
}

// noopNotifier is used when notifications are disabled: no NOTIFY_EMAIL, or
// any required SMTP_* variable missing (Requirements 4.6, 4.9).
type noopNotifier struct{}

func (noopNotifier) NotifyFailure(*Task) error { return nil }

// EmailNotifier sends failure notifications via SMTP.
type EmailNotifier struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
	To       string
}

// NewNotifier returns an EmailNotifier configured from cfg if notifications
// are enabled, or a no-op Notifier otherwise.
func NewNotifier(cfg Config) Notifier {
	if !cfg.NotificationsEnabled {
		return noopNotifier{}
	}
	return &EmailNotifier{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		User:     cfg.SMTPUser,
		Password: cfg.SMTPPassword,
		From:     cfg.SMTPFrom,
		To:       cfg.NotifyEmail,
	}
}

// NotifyFailure sends an email to n.To containing the Task's name, ID,
// command, and exit code (Requirement 4.3). It never retries on failure;
// callers are responsible for logging a delivery error (Requirement 4.5).
func (n *EmailNotifier) NotifyFailure(task *Task) error {
	addr := fmt.Sprintf("%s:%s", n.Host, n.Port)
	auth := smtp.PlainAuth("", n.User, n.Password, n.Host)

	exitCode := 0
	if task.ExitCode != nil {
		exitCode = *task.ExitCode
	}

	msg := buildFailureMessage(n.From, n.To, task, exitCode)

	return smtp.SendMail(addr, auth, n.From, []string{n.To}, msg)
}

// buildFailureMessage constructs the raw RFC 5322 email message announcing
// task's failure, sent from and to.
func buildFailureMessage(from, to string, task *Task, exitCode int) []byte {
	subject := fmt.Sprintf("Taskline: task %s failed", task.Name)
	body := fmt.Sprintf(
		"Task %s (%s) failed.\r\n\r\nCommand: %s\r\nExit code: %d\r\n",
		task.Name, task.ID, task.Command, exitCode,
	)
	return []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s", from, to, subject, body))
}
