package logger

import (
	"fmt"
	"log"
)

// Logger builds on top of log.Logger with some helpful methods.
type Logger struct {
	*log.Logger
	DebugMode bool
}

// Debugf logs when debug mode is active, works like Printf.
func (l Logger) Debugf(format string, v ...interface{}) {
	if l.DebugMode {
		l.Printf(fmt.Sprintf("DEBUG: %s", format), v...)
	}
}

// Infof logs a message with an "INFO:" prefix, works like Printf.
func (l Logger) Infof(format string, v ...interface{}) {
	l.Printf(fmt.Sprintf("INFO: %s", format), v...)
}

// Warningf logs a message with an "WARNING:" prefix, works like Printf.
func (l Logger) Warningf(format string, v ...interface{}) {
	l.Printf(fmt.Sprintf("INFO: %s", format), v...)
}

// Errorf logs a message with an "WARNING:" prefix, works like Printf.
func (l Logger) Errorf(format string, v ...interface{}) {
	l.Printf(fmt.Sprintf("ERROR: %s", format), v...)
}
