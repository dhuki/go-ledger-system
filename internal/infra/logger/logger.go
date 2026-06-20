package logger

import (
	"context"
	"fmt"
	"maps"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
	"go.elastic.co/apm/module/apmlogrus"
)

var once sync.Once
var l *logrus.Logger
var e *logrus.Entry

type XTraceID string

var (
	XTraceId XTraceID = "X-Trace-ID"
)

const (
	maximumCallerDepth int = 25
	knownLogrusFrames  int = 4

	TraceID = "trace-id"
)

func init() {
	once.Do(func() {
		l = logrus.New()
		l.SetFormatter(&logrus.JSONFormatter{
			CallerPrettyfier: func(f *runtime.Frame) (string, string) {
				pcs := make([]uintptr, maximumCallerDepth)
				depth := runtime.Callers(knownLogrusFrames, pcs)
				frames := runtime.CallersFrames(pcs[:depth])

				for frame, ok := frames.Next(); ok; frame, ok = frames.Next() {
					pkg, currentPkg := getPackageName(frame.Function), getPackageName(f.Function)
					if pkg != currentPkg && pkg != "github.com/sirupsen/logrus" {
						f = &frame
						break
					}
				}

				lastSlash := strings.LastIndex(f.Function, "/")
				funcname := f.Function[lastSlash+1:]
				if dotIdx := strings.Index(funcname, "."); dotIdx >= 0 {
					funcname = funcname[dotIdx+1:]
				}
				return funcname, fmt.Sprintf("%s:%d", f.File, f.Line)
			},
		})
		l.SetReportCaller(true)
		l.SetOutput(os.Stdout)
		l.AddHook(&apmlogrus.Hook{})
		e = logrus.NewEntry(l)
	})
}

func getPackageName(f string) string {
	for {
		lastPeriod := strings.LastIndex(f, ".")
		lastSlash := strings.LastIndex(f, "/")
		if lastPeriod > lastSlash {
			f = f[:lastPeriod]
		} else {
			break
		}
	}
	return f
}

func Info(ctx context.Context, args ...interface{}) {
	log(ctx, logrus.InfoLevel, args...)
}

func Error(ctx context.Context, args ...interface{}) {
	log(ctx, logrus.ErrorLevel, args...)
}

func Fatal(ctx context.Context, args ...interface{}) {
	log(ctx, logrus.FatalLevel, args...)
}

func Debug(ctx context.Context, args ...interface{}) {
	log(ctx, logrus.DebugLevel, args...)
}

func Warn(ctx context.Context, args ...interface{}) {
	log(ctx, logrus.WarnLevel, args...)
}

func SetCustomField(ctx context.Context, field logrus.Fields) {
	e = e.WithFields(field)
}

func log(ctx context.Context, level logrus.Level, args ...interface{}) {
	fields, msg := format(ctx, args...)
	e.WithFields(fields).Log(level, strings.Join(msg, " "))
}

func format(ctx context.Context, args ...interface{}) (logrus.Fields, []string) {
	var msg []string
	defaultFields := make(logrus.Fields, 0)
	defaultFields[TraceID] = ctx.Value(XTraceId)
	for _, v := range args {
		switch val := any(v).(type) {
		case error:
			msg = append(msg, fmt.Sprintf("cause: %v", val))
		case string, int, int64, float64, bool:
			msg = append(msg, fmt.Sprintf("%v", val))
		case logrus.Fields, map[string]interface{}:
			if custFields, ok := val.(logrus.Fields); ok {
				maps.Copy(defaultFields, custFields)
			}
		}
	}
	return defaultFields, msg
}
