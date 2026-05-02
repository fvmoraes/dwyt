package log

import (
	"fmt"
	"os"
	"sync"
	"time"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

var levelNames = map[Level]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
}

type Entry struct {
	Time    time.Time `json:"time"`
	Level   Level     `json:"level"`
	Message string    `json:"message"`
	Fields  Fields    `json:"fields,omitempty"`
}

type Fields map[string]interface{}

type Logger struct {
	mu     sync.Mutex
	level  Level
	writer *os.File
}

var std = &Logger{level: INFO, writer: os.Stderr}

func SetLevel(l Level) { std.level = l }

func SetOutput(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	std.mu.Lock()
	std.writer = f
	std.mu.Unlock()
	return nil
}

func (l *Logger) log(level Level, msg string, fields Fields) {
	if level < l.level {
		return
	}
	e := &Entry{
		Time:    time.Now(),
		Level:   level,
		Message: msg,
		Fields:  fields,
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.writer, "[%s] %s  %s", e.Time.Format(time.RFC3339), levelNames[level], msg)
	if len(fields) > 0 {
		fmt.Fprintf(l.writer, "  %v", fields)
	}
	fmt.Fprintln(l.writer)
}

func Debug(msg string, fields ...Fields)  { std.log(DEBUG, msg, merge(fields)) }
func Info(msg string, fields ...Fields)   { std.log(INFO, msg, merge(fields)) }
func Warn(msg string, fields ...Fields)   { std.log(WARN, msg, merge(fields)) }
func Error(msg string, fields ...Fields)  { std.log(ERROR, msg, merge(fields)) }

func merge(fields []Fields) Fields {
	if len(fields) == 0 {
		return nil
	}
	return fields[0]
}
