package logging

import (
	"errors"
	"io"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New builds the process logger. TAKO_LOG_LEVEL accepts debug, info, warn,
// error, dpanic, panic, or fatal and defaults to info.
func New(mode string) (*zap.Logger, error) {
	return NewWithOutput(mode, os.Stderr)
}

// NewWithOutput builds the process logger using output as its sink.
func NewWithOutput(mode string, output io.Writer) (*zap.Logger, error) {
	level := zapcore.InfoLevel
	if value := os.Getenv("TAKO_LOG_LEVEL"); value != "" {
		if err := level.UnmarshalText([]byte(value)); err != nil {
			return nil, errors.New("invalid TAKO_LOG_LEVEL: " + value)
		}
	}
	encoder := zap.NewProductionEncoderConfig()
	encoder.TimeKey = ""
	encoder.CallerKey = ""
	encoder.EncodeLevel = zapcore.CapitalLevelEncoder
	encoder.EncodeDuration = zapcore.StringDurationEncoder

	options := []zap.Option{zap.Fields(zap.String("mode", mode))}
	if level <= zapcore.DebugLevel {
		encoder.CallerKey = "caller"
		options = append(options, zap.AddCaller())
	}

	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoder),
		zapcore.Lock(zapcore.AddSync(output)),
		level,
	)
	return zap.New(core, options...), nil
}
