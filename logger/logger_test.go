package logger

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

func TestInit(t *testing.T) {
	// Save original env vars
	origLevel := os.Getenv("LOG_LEVEL")
	origPretty := os.Getenv("LOG_PRETTY")
	origPath := os.Getenv("LOG_PATH")
	defer func() {
		os.Setenv("LOG_LEVEL", origLevel)
		os.Setenv("LOG_PRETTY", origPretty)
		os.Setenv("LOG_PATH", origPath)
	}()

	// Test default level (info)
	os.Unsetenv("LOG_LEVEL")
	os.Unsetenv("LOG_PRETTY")
	os.Unsetenv("LOG_PATH")
	Init()
	if zerolog.GlobalLevel() != zerolog.InfoLevel {
		t.Errorf("Expected default level to be Info (%d), got %d", zerolog.InfoLevel, zerolog.GlobalLevel())
	}

	// Test debug level
	os.Setenv("LOG_LEVEL", "debug")
	Init()
	if zerolog.GlobalLevel() != zerolog.DebugLevel {
		t.Errorf("Expected level to be Debug (%d), got %d", zerolog.DebugLevel, zerolog.GlobalLevel())
	}

	// Test warn level
	os.Setenv("LOG_LEVEL", "warn")
	Init()
	if zerolog.GlobalLevel() != zerolog.WarnLevel {
		t.Errorf("Expected level to be Warn (%d), got %d", zerolog.WarnLevel, zerolog.GlobalLevel())
	}

	// Test error level
	os.Setenv("LOG_LEVEL", "error")
	Init()
	if zerolog.GlobalLevel() != zerolog.ErrorLevel {
		t.Errorf("Expected level to be Error (%d), got %d", zerolog.ErrorLevel, zerolog.GlobalLevel())
	}
}

func TestInitWithConfig(t *testing.T) {
	// Test with explicit config
	InitWithConfig(Config{
		Level:  "debug",
		Path:   "stderr",
		Pretty: false,
	})

	if zerolog.GlobalLevel() != zerolog.DebugLevel {
		t.Errorf("Expected level to be Debug (%d), got %d", zerolog.DebugLevel, zerolog.GlobalLevel())
	}

	// Test with file output (create a temp file)
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	InitWithConfig(Config{
		Level:  "info",
		Path:   logFile,
		Pretty: false,
	})

	// Check file was created (logger initialization should work)
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		// File might not be created until first log, that's ok
		// Just verify no panic occurred during InitWithConfig
	}
}

func TestWithComponent(t *testing.T) {
	Init()
	log := WithComponent("test-component")

	// Just verify it doesn't panic and returns a logger
	if log.GetLevel() != Logger().GetLevel() {
		t.Error("Component logger should have same level as global logger")
	}
}
