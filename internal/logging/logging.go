package logging

import (
	"errors"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New builds the process logger. TAKO_LOG_LEVEL accepts debug, info, warn,
// error, dpanic, panic, or fatal and defaults to info.
func New(mode string) (*zap.Logger, error) {
	level := zapcore.InfoLevel
	if value := os.Getenv("TAKO_LOG_LEVEL"); value != "" {
		if err := level.UnmarshalText([]byte(value)); err != nil {
			return nil, errors.New("invalid TAKO_LOG_LEVEL: " + value)
		}
	}
	encoder := zap.NewProductionEncoderConfig()
	encoder.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	encoder.EncodeLevel = zapcore.LowercaseLevelEncoder
	core := zapcore.NewCore(zapcore.NewJSONEncoder(encoder), zapcore.Lock(os.Stderr), level)
	return zap.New(core, zap.AddCaller(), zap.Fields(
		zap.String("service", "tako"),
		zap.String("mode", mode),
	)), nil
}
