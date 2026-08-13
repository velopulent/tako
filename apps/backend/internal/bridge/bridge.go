// Package bridge runs inside an authenticated UNIX user session. The framed RPC
// transport is intentionally small so module adapters can move behind this
// process without changing HTTP contracts.
package bridge

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type frame struct {
	ID      string          `json:"id"`
	Method  string          `json:"method,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// Run serves framed bridge RPC until input closes or an I/O error occurs.
func Run(input io.Reader, output io.Writer, errorOutput io.Writer) error {
	encoder := zap.NewProductionEncoderConfig()
	encoder.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	logger := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(encoder), zapcore.AddSync(errorOutput), zapcore.InfoLevel), zap.AddCaller()).With(
		zap.String("service", "tako"),
		zap.String("mode", "bridge"),
	)
	defer func() { _ = logger.Sync() }()
	logger.Info("bridge started")
	reader := bufio.NewReader(input)
	writer := bufio.NewWriter(output)
	for {
		message, err := readFrame(reader)
		if err != nil {
			if err == io.EOF {
				logger.Info("bridge stopped")
				return nil
			}
			logger.Error("bridge frame read failed", zap.Error(err))
			return err
		}
		response := frame{ID: message.ID, Error: "unsupported-method"}
		if message.Method == "ping" {
			response.Error = ""
			response.Payload = json.RawMessage(`{"ok":true}`)
		}
		if err := writeFrame(writer, response); err != nil {
			logger.Error("bridge frame write failed", zap.Error(err))
			return err
		}
	}
}

func readFrame(reader io.Reader) (frame, error) {
	var size uint32
	if err := binary.Read(reader, binary.BigEndian, &size); err != nil {
		return frame{}, err
	}
	if size == 0 || size > 1<<20 {
		return frame{}, fmt.Errorf("invalid frame size: %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return frame{}, err
	}
	var message frame
	return message, json.Unmarshal(payload, &message)
}

func writeFrame(writer *bufio.Writer, message frame) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if err := binary.Write(writer, binary.BigEndian, uint32(len(payload))); err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	return writer.Flush()
}
