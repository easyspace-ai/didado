package services

import (
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type LogLevel string

const (
	LogInfo  LogLevel = "INFO"
	LogWarn  LogLevel = "WARN"
	LogError LogLevel = "ERROR"
	LogTrade LogLevel = "TRADE"
)

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*m`)

type FileLogger struct {
	logDir string
}

func NewFileLogger(logDir string) *FileLogger {
	if logDir == "" {
		logDir = filepath.Join(mustCwd(), "logs")
	}
	return &FileLogger{logDir: logDir}
}

func (l *FileLogger) Log(level LogLevel, message string) {
	_ = os.MkdirAll(l.logDir, 0o755)

	date := time.Now().Format("2006-01-02")
	path := filepath.Join(l.logDir, "bot-"+date+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		// 最后手段：无法写文件就忽略（不panic）
		return
	}
	defer f.Close()

	ts := time.Now().UTC().Format(time.RFC3339)
	clean := ansiRegexp.ReplaceAllString(message, "")
	_, _ = f.WriteString("[" + ts + "] [" + string(level) + "] " + clean + "\n")
}

func mustCwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
