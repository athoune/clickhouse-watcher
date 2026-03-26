package logger

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func TestInit(t *testing.T) {
	// Save original env vars
	origLevel := os.Getenv("LOG_LEVEL")
	origPretty := os.Getenv("LOG_PRETTY")
	defer func() {
		os.Setenv("LOG_LEVEL", origLevel)
		os.Setenv("LOG_PRETTY", origPretty)
	}()

	// Test default level (info)
	os.Unsetenv("LOG_LEVEL")
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

func TestWithComponent(t *testing.T) {
	Init()
	log := WithComponent("test-component")

	// Just verify it doesn't panic and returns a logger
	if log.GetLevel() != Logger().GetLevel() {
		t.Error("Component logger should have same level as global logger")
	}
}
