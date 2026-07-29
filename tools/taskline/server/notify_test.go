package main

import (
	"fmt"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeSMTPServer is a minimal SMTP server accepting a single AUTH PLAIN
// session and capturing the message body, just enough to exercise
// EmailNotifier.NotifyFailure end-to-end without a real mail relay.
type fakeSMTPServer struct {
	listener net.Listener
	received chan string
}

func startFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTPServer{listener: ln, received: make(chan string, 1)}
	go s.serveOne(t)
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *fakeSMTPServer) addr() (host, port string) {
	host, port, _ = net.SplitHostPort(s.listener.Addr().String())
	return
}

func (s *fakeSMTPServer) serveOne(t *testing.T) {
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()

	tp := textproto.NewConn(conn)
	tp.PrintfLine("220 fake.smtp ESMTP")

	var body strings.Builder
	inData := false
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		if inData {
			if line == "." {
				inData = false
				tp.PrintfLine("250 OK: queued")
				s.received <- body.String()
				continue
			}
			body.WriteString(line)
			body.WriteString("\n")
			continue
		}

		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			tp.PrintfLine("250-fake.smtp greets you")
			tp.PrintfLine("250 AUTH PLAIN")
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			tp.PrintfLine("235 authenticated")
		case strings.HasPrefix(upper, "MAIL FROM"):
			tp.PrintfLine("250 OK")
		case strings.HasPrefix(upper, "RCPT TO"):
			tp.PrintfLine("250 OK")
		case upper == "DATA":
			tp.PrintfLine("354 send message")
			inData = true
		case upper == "QUIT":
			tp.PrintfLine("221 bye")
			return
		default:
			tp.PrintfLine("500 unrecognized command")
		}
	}
}

func TestEmailNotifier_NotifyFailureSendsExpectedMessage(t *testing.T) {
	server := startFakeSMTPServer(t)
	host, port := server.addr()

	exitCode := 7
	task := &Task{
		ID:       "abc12345",
		Name:     "brave-tiger",
		Command:  "npm run build",
		Status:   TaskFailed,
		ExitCode: &exitCode,
	}

	n := &EmailNotifier{
		Host:     host,
		Port:     port,
		User:     "user@example.com",
		Password: "secret",
		From:     "taskline@example.com",
		To:       "ops@example.com",
	}

	if err := n.NotifyFailure(task); err != nil {
		t.Fatalf("NotifyFailure: %v", err)
	}

	select {
	case body := <-server.received:
		for _, want := range []string{task.Name, task.ID, task.Command, strconv.Itoa(exitCode)} {
			if !strings.Contains(body, want) {
				t.Errorf("message body missing %q:\n%s", want, body)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive a message in time")
	}
}

func TestNewNotifier_DisabledReturnsNoop(t *testing.T) {
	cfg := Config{NotificationsEnabled: false}
	n := NewNotifier(cfg)
	if _, ok := n.(noopNotifier); !ok {
		t.Fatalf("expected noopNotifier, got %T", n)
	}
	if err := n.NotifyFailure(&Task{}); err != nil {
		t.Fatalf("noopNotifier.NotifyFailure returned error: %v", err)
	}
}

func TestNewNotifier_EnabledReturnsEmailNotifier(t *testing.T) {
	cfg := Config{
		NotificationsEnabled: true,
		NotifyEmail:          "ops@example.com",
		SMTPHost:             "smtp.example.com",
		SMTPPort:             "587",
		SMTPUser:             "user@example.com",
		SMTPPassword:         "secret",
		SMTPFrom:             "from@example.com",
	}
	n := NewNotifier(cfg)
	email, ok := n.(*EmailNotifier)
	if !ok {
		t.Fatalf("expected *EmailNotifier, got %T", n)
	}
	if email.Host != cfg.SMTPHost || email.Port != cfg.SMTPPort || email.User != cfg.SMTPUser ||
		email.Password != cfg.SMTPPassword || email.From != cfg.SMTPFrom || email.To != cfg.NotifyEmail {
		t.Fatalf("EmailNotifier fields do not match config: %+v", email)
	}
}

func TestBuildFailureMessage_ContainsRequiredFields(t *testing.T) {
	exitCode := 2
	task := &Task{ID: "id12345", Name: "calm-river", Command: "npm test"}
	msg := string(buildFailureMessage("from@example.com", "to@example.com", task, exitCode))

	for _, want := range []string{"from@example.com", "to@example.com", task.ID, task.Name, task.Command, fmt.Sprintf("%d", exitCode)} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
}
