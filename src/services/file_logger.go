package services

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileLogger 文件日志记录器
type FileLogger struct {
	logDir string
}

var fileLoggerInstance *FileLogger

func init() {
	fileLoggerInstance = &FileLogger{
		logDir: "logs",
	}
}

// ensureDir 确保日志目录存在
func (f *FileLogger) ensureDir() {
	if _, err := os.Stat(f.logDir); os.IsNotExist(err) {
		os.MkdirAll(f.logDir, 0755)
	}
}

// getFilePath 获取日志文件路径
func (f *FileLogger) getFilePath() string {
	f.ensureDir()
	date := time.Now().Format("2006-01-02")
	return filepath.Join(f.logDir, fmt.Sprintf("bot-%s.log", date))
}

// Log 记录日志
func (f *FileLogger) Log(level string, message string) {
	filePath := f.getFilePath()
	timestamp := time.Now().Format(time.RFC3339)
	line := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, message)

	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("❌ Failed to write to log file: %v\n", err)
		return
	}
	defer file.Close()

	file.WriteString(line)
}
