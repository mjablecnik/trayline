package main

import (
	"fmt"
	"os"
	"time"
)

// logf writes a single structured log line to stdout in the
// "YYYY-MM-DD HH:MM:SS [LEVEL] message" format (Requirement 18.3). All
// server output, logs included, goes to stdout only (Requirement 18.7).
func logf(level, format string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, "%s [%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), level, fmt.Sprintf(format, args...))
}

func logInfo(format string, args ...interface{}) {
	logf("INFO", format, args...)
}

func logWarn(format string, args ...interface{}) {
	logf("WARN", format, args...)
}

func logError(format string, args ...interface{}) {
	logf("ERROR", format, args...)
}
