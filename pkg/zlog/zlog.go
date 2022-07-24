package zlog

import (
	"fmt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"time"
)

var (
	logger    *zap.Logger
	errLogger *zap.Logger
)

// ParseLevel - 接受一个字符串，返回zapcore.Level常量
func ParseLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.DebugLevel
	}
}

func NewZapLog(level, encoder string) {
	zapConfig := zapcore.EncoderConfig{
		TimeKey:       "datetime",
		LevelKey:      "level",
		NameKey:       "logger",
		CallerKey:     "caller",
		MessageKey:    "message",
		StacktraceKey: "stacktrace",
		LineEnding:    zapcore.DefaultLineEnding, // 行结束符\n
		EncodeLevel:   zapcore.CapitalLevelEncoder,
		EncodeTime: func(time time.Time, encoder zapcore.PrimitiveArrayEncoder) {
			encoder.AppendString(time.Format("2006-01-02 15:04:05"))
		},
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	var zapEncoder zapcore.Encoder
	if encoder == "json" {
		zapEncoder = zapcore.NewJSONEncoder(zapConfig)
	}
	zapEncoder = zapcore.NewConsoleEncoder(zapConfig)

	writeSyncer := zapcore.AddSync(os.Stdout)
	core := zapcore.NewCore(zapEncoder, writeSyncer, ParseLevel(level)) // 日志输出级别

	errLogger = zap.New(core,
		zap.AddCaller(),                       // 打印文件名和行号
		zap.AddCallerSkip(1),                  // 封装了一层日志方法
		zap.AddStacktrace(zapcore.ErrorLevel), // 添加错误信息堆栈的级别
	)

	logger = zap.New(core,
		zap.AddCaller(),      // 打印文件名和行号
		zap.AddCallerSkip(1), // 封装了一层日志方法
		zap.AddStacktrace(zapcore.PanicLevel),
	)
}

func Debug(msg string, fields ...zap.Field) {
	logger.Debug(msg, fields...)
}

func Info(msg string, fields ...zap.Field) {
	logger.Info(msg, fields...)
}

func Warn(msg string, fields ...zap.Field) {
	logger.Warn(msg, fields...)
}

type withStack interface {
	Format(s fmt.State, verb rune)
}

func Error(err error, fields ...zap.Field) {
	// 接口断言
	if _, ok := err.(withStack); ok {
		logger.Error(fmt.Sprintf("%+v", err), fields...)
		return
	}
	errLogger.Error(err.Error(), fields...)
}

func Sync() {
	_ = errLogger.Sync()
	_ = logger.Sync()
}
