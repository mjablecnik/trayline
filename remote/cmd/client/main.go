package main

import (
	"os"
	"os/signal"
	"syscall"
)

// globalSigCh receives SIGINT/SIGTERM for non-chat contexts.
// The chat handler calls signal.Stop(globalSigCh) before installing its own handler.
var globalSigCh = make(chan os.Signal, 1)

func main() {
	signal.Notify(globalSigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-globalSigCh
		if sig == syscall.SIGTERM {
			os.Exit(0)
		}
		// SIGINT
		os.Exit(130)
	}()

	os.Exit(Dispatch(os.Args[1:]))
}
