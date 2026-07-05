package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ergochat/readline"
	"github.com/gorilla/websocket"
)

// handleChat parses chat subcommand flags, connects via WebSocket, and runs the chat loop.
func handleChat(args []string, cfg *Config) int {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	agentFlag   := fs.String("agent", "", "")
	modelFlag   := fs.String("model", "", "")
	systemFlag  := fs.String("system", "", "")
	sessionFlag := fs.String("session", "", "")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Print(subcommandUsage["chat"])
			return 0
		}
		fmt.Fprintf(os.Stderr, "Error: %s\nRun with --help for usage information.\n", err)
		return 2
	}

	if *agentFlag == "" {
		fmt.Fprintf(os.Stderr, "Error: --agent flag is required.\nRun with --help for usage information.\n")
		return 2
	}

	wsPath := buildChatPath(*sessionFlag)
	params := buildChatParams(*agentFlag, *modelFlag, *systemFlag)

	// Take over signal handling from the global handler.
	signal.Stop(globalSigCh)
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	client := NewAPIClient(cfg)
	conn, httpResp, err := client.DialWebSocket(wsPath, params)
	if err != nil {
		if apiErr, ok := err.(*APIError); ok {
			switch apiErr.StatusCode {
			case http.StatusNotFound:
				sid := *sessionFlag
				if sid == "" {
					sid = "unknown"
				}
				fmt.Fprintf(os.Stderr, "Error: Session %s not found or is no longer active.\n", sid)
				return 1
			case http.StatusConflict:
				sid := *sessionFlag
				if sid == "" {
					sid = "unknown"
				}
				fmt.Fprintf(os.Stderr, "Error: Session %s is already in use by another client.\n", sid)
				return 1
			}
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = httpResp
	defer conn.Close()

	return chatLoop(conn, cfg, os.Stdin, sigCh)
}

// chatLoop runs the interactive WebSocket chat session event loop.
// stdin and sigCh are parameters to allow test injection.
func chatLoop(conn *websocket.Conn, cfg *Config, stdin io.Reader, sigCh <-chan os.Signal) int {
	type serverResult struct {
		msg *WSServerMessage
		err error
	}
	serverCh := make(chan serverResult, 16)
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				serverCh <- serverResult{err: err}
				return
			}
			var msg WSServerMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				serverCh <- serverResult{err: err}
				return
			}
			serverCh <- serverResult{msg: &msg}
		}
	}()

	type inputResult struct {
		line string
		eof  bool
		err  error
	}
	inputCh := make(chan inputResult, 1)

	// Use readline for interactive terminals (provides arrow keys, cursor movement, history).
	// Fall back to bufio.Scanner for non-TTY input (pipes, tests).
	var rl *readline.Instance
	if f, ok := stdin.(*os.File); ok && isTerminalFd(f) {
		fmtr := NewFormatter()
		prompt := fmtr.Green(os.Stderr, "> ")
		var err error
		rl, err = readline.NewFromConfig(&readline.Config{
			Prompt:          prompt,
			Stdin:           f,
			Stdout:          os.Stderr,
			Stderr:          os.Stderr,
			InterruptPrompt: "",
			EOFPrompt:       "",
		})
		if err == nil {
			go func() {
				for {
					line, err := rl.Readline()
					if err != nil {
						if err == readline.ErrInterrupt {
							// Ignore — signal handler deals with SIGINT
							continue
						}
						inputCh <- inputResult{eof: true}
						return
					}
					inputCh <- inputResult{line: line}
				}
			}()
		} else {
			// readline init failed, fall back to scanner
			rl = nil
		}
	}

	if rl == nil {
		go func() {
			scanner := bufio.NewScanner(stdin)
			for scanner.Scan() {
				inputCh <- inputResult{line: scanner.Text()}
			}
			if err := scanner.Err(); err != nil {
				inputCh <- inputResult{err: err}
			} else {
				inputCh <- inputResult{eof: true}
			}
		}()
	}

	// Close readline on exit to restore terminal state.
	defer func() {
		if rl != nil {
			rl.Close()
		}
	}()

	sendJSON := func(msgType, prompt string) error {
		msg := WSClientMessage{Type: msgType, Prompt: prompt}
		data, _ := json.Marshal(msg)
		return conn.WriteMessage(websocket.TextMessage, data)
	}

	// waitForTerminated drains serverCh until a "terminated" message arrives or timeout elapses.
	waitForTerminated := func() {
		timer := time.After(5 * time.Second)
		for {
			select {
			case sr := <-serverCh:
				if sr.err != nil || (sr.msg != nil && sr.msg.Type == "terminated") {
					return
				}
			case <-timer:
				return
			}
		}
	}

	// showPrompt displays the input prompt. When readline is active, it refreshes
	// readline's prompt display; otherwise it writes "> " to stderr.
	showPrompt := func() {
		if cfg.Quiet {
			return
		}
		if rl != nil {
			// readline displays its own prompt on each Readline() call,
			// but we can trigger a refresh after output.
			return
		}
		PrintPrompt(os.Stderr)
	}

	interruptCount := 0
	fmtr := NewFormatter()
	streaming := false
	var doneTimer *time.Timer
	doneTimerCh := func() <-chan time.Time {
		if doneTimer != nil {
			return doneTimer.C
		}
		return nil
	}

	for {
		select {
		case <-doneTimerCh():
			doneTimer = nil
			showPrompt()

		case sr := <-serverCh:
			if sr.err != nil {
				fmt.Fprintln(os.Stderr, "Error: WebSocket connection closed unexpectedly.")
				return 1
			}
			msg := sr.msg
			switch msg.Type {
			case "session_started", "session_resumed":
				if !cfg.Quiet {
					fmt.Fprintf(os.Stderr, "Session ID: %s\n", msg.SessionID)
					showPrompt()
				}
			case "output":
				if doneTimer != nil {
					doneTimer.Stop()
					doneTimer = nil
					// This is a follow-up response chunk — add separator
					fmt.Fprint(os.Stderr, fmtr.Yellow(os.Stderr, "  ···\n"))
				}
				if !streaming {
					streaming = true
					fmt.Fprint(os.Stderr, fmtr.Cyan(os.Stderr, "🤖 "))
				}
				fmt.Print(msg.Data)
			case "done":
				fmt.Print("\n\n")
				streaming = false
				interruptCount = 0
				doneTimer = time.NewTimer(150 * time.Millisecond)
			case "error":
				fmt.Fprintln(os.Stderr, msg.Message)
			case "context_compacted":
				if !cfg.Quiet {
					fmt.Fprintln(os.Stderr, "Info: Context was compacted to fit within limits.")
				}
			case "terminated":
				return 0
			}

		case il := <-inputCh:
			if il.eof {
				return 0
			}
			if il.err != nil {
				fmt.Fprintf(os.Stderr, "Error: Failed to read input: %v\n", il.err)
				return 1
			}
			if !shouldSendLine(il.line) {
				continue
			}
			line := strings.TrimSpace(il.line)
			if line == "/quit" {
				sendJSON("terminate", "")
				waitForTerminated()
				return 0
			}
			if err := sendJSON("message", line); err != nil {
				fmt.Fprintf(os.Stderr, "Error: Failed to send message: %v\n", err)
				return 1
			}

		case sig := <-sigCh:
			switch sig {
			case syscall.SIGTERM:
				sendJSON("terminate", "")
				waitForTerminated()
				return 0
			case syscall.SIGINT:
				interruptCount++
				if interruptCount >= 2 {
					return 130
				}
				sendJSON("interrupt", "")
			}
		}
	}
}

// shouldSendLine reports whether a line of user input should be sent as a message.
// Empty and whitespace-only lines are discarded.
func shouldSendLine(line string) bool {
	return strings.TrimSpace(line) != ""
}

// buildChatPath returns the WebSocket path for a new session or reconnection.
func buildChatPath(sessionID string) string {
	if sessionID != "" {
		return "/chat/" + sessionID
	}
	return "/chat"
}

// buildChatParams constructs query parameters for a chat WebSocket connection.
// Agent is always included; model and system are included only when non-empty.
func buildChatParams(agent, model, system string) url.Values {
	params := url.Values{}
	params.Set("agent", agent)
	if model != "" {
		params.Set("model", model)
	}
	if system != "" {
		params.Set("system", system)
	}
	return params
}
