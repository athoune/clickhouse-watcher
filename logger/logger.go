// Package logger provides centralized logging configuration using zerolog.
package logger

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// discardWriter is a writer that discards all output
type discardWriter struct{}

func (d discardWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// Config holds logging configuration
type Config struct {
	Level  string
	Path   string
	Pretty bool
}

// Init initializes the global zerolog configuration.
// Uses environment variables as fallback:
//
//	LOG_LEVEL - debug, info, warn, error
//	LOG_PRETTY - true/false for pretty printing
//	LOG_PATH - file path or "stdout"/"stderr"
func Init() {
	cfg := Config{
		Level:  os.Getenv("LOG_LEVEL"),
		Path:   os.Getenv("LOG_PATH"),
		Pretty: os.Getenv("LOG_PRETTY") == "true",
	}
	InitWithConfig(cfg)
}

// InitWithConfig initializes the logger with explicit configuration.
// Path can be:
//   - "" (empty) -> stderr
//   - "stdout" -> stdout
//   - "stderr" -> stderr
//   - "discard" / "null" -> discard all logs (for TUI applications)
//   - file path -> write to file
func InitWithConfig(cfg Config) {
	// Configure time format
	zerolog.TimeFieldFormat = time.RFC3339

	// Set log level
	level := strings.ToLower(cfg.Level)
	switch level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info", "":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	case "fatal":
		zerolog.SetGlobalLevel(zerolog.FatalLevel)
	case "disabled", "none":
		zerolog.SetGlobalLevel(zerolog.Disabled)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	// Determine output writer
	var output io.Writer
	switch strings.ToLower(cfg.Path) {
	case "", "stderr":
		output = os.Stderr
	case "stdout":
		output = os.Stdout
	case "discard", "null":
		output = discardWriter{}
	default:
		// Open log file
		file, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			// Fall back to stderr on error
			output = os.Stderr
			log.Error().
				Err(err).
				Str("path", cfg.Path).
				Msg("Failed to open log file, using stderr")
		} else {
			output = file
		}
	}

	// Configure output format
	if cfg.Pretty {
		log.Logger = zerolog.New(zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: time.RFC3339,
		}).With().Timestamp().Logger()
	} else {
		log.Logger = zerolog.New(output).With().Timestamp().Logger()
	}

	log.Debug().
		Str("level", level).
		Str("path", cfg.Path).
		Bool("pretty", cfg.Pretty).
		Msg("Logger initialized")
}

// Logger returns a pre-configured logger instance.
func Logger() zerolog.Logger {
	return log.Logger
}

// WithComponent returns a logger with a component field for tracing.
func WithComponent(component string) zerolog.Logger {
	return log.Logger.With().Str("component", component).Logger()
}
