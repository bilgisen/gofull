package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
)

var Log *zap.Logger

func InitLogger(env string) {
	var cfg zap.Config

	if env == "production" {
		cfg = zap.Config{
			Encoding:         "json",
			Level:            zap.NewAtomicLevelAt(zapcore.InfoLevel),
			OutputPaths:      []string{"stdout"},
			ErrorOutputPaths: []string{"stderr"},
			EncoderConfig: zapcore.EncoderConfig{
				TimeKey:        "time",
				LevelKey:       "level",
				MessageKey:     "message",
				CallerKey:      "caller",
				EncodeTime:     zapcore.ISO8601TimeEncoder,
				EncodeLevel:    zapcore.CapitalLevelEncoder,
				EncodeCaller:   zapcore.ShortCallerEncoder,
				EncodeDuration: zapcore.StringDurationEncoder,
			},
		}
	} else {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	var err error
	Log, err = cfg.Build()
	if err != nil {
		panic("failed to initialize zap logger: " + err.Error())
	}

	Log.Info("Logger initialized", zap.String("env", env))
}

func Sync() {
	if Log != nil {
		_ = Log.Sync()
	}
}
