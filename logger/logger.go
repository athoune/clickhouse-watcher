// Package logger provides centralized logging configuration using zerolog.
package logger

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Init initializes the global zerolog configuration.
// Log level is controlled via the LOG_LEVEL environment variable (debug, info, warn, error).
// Pretty printing is enabled via LOG_PRETTY=true.
func Init() {
	// Configure time format
	zerolog.TimeFieldFormat = time.RFC3339

	// Set log level from environment
	level := strings.ToLower(os.Getenv("LOG_LEVEL"))
	switch level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info", "":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	// Configure output format
	if os.Getenv("LOG_PRETTY") == "true" {
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		})
	}

	log.Debug().Str("level", level).Msg("Logger initialized")
}

// Logger returns a pre-configured logger instance.
func Logger() zerolog.Logger {
	return log.Logger
}

// WithComponent returns a logger with a component field for tracing.
func WithComponent(component string) zerolog.Logger {
	return log.Logger.With().Str("component", component).Logger()
}
