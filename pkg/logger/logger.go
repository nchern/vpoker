package logger

import (
	"fmt"
	"log"
	"os"
)

const defaultLogFlags = log.LstdFlags | log.Llongfile | log.Lmsgprefix

var (
	// Error represents a log sink for error messages
	Error Interface

	// Info represents a log sink for info messages
	Info Interface

	// Debug represents a log sink for debug messages
	Debug Interface

	nullLoggerImpl = &nullLogger{}
)

func init() {
	log.SetFlags(defaultLogFlags)

	Debug = newLogger("DEBUG", 2)
	Error = newLogger("ERROR", 2)
	Info = newLogger("INFO", 2)
}

// Null returns null logger implementation
func Null() Interface { return nullLoggerImpl }

// Interface abstracts logging interface
type Interface interface {
	Println(v ...interface{})
	Printf(format string, v ...interface{})
}

type nullLogger struct{}

func (l *nullLogger) Println(v ...interface{}) {}

func (l *nullLogger) Printf(format string, v ...interface{}) {}

type loggerImpl struct {
	depth int
	log   *log.Logger
}

func newLogger(prefix string, depth int) *loggerImpl {
	return &loggerImpl{
		depth: depth,
		log:   log.New(os.Stderr, prefix+" ", inferLogFlags()),
	}
}

func (l *loggerImpl) SetDepth(depth int) { l.depth = depth }

func (l *loggerImpl) Println(v ...interface{}) {
	l.log.Output(l.depth, fmt.Sprintln(v...))
}

func (l *loggerImpl) Printf(format string, v ...interface{}) {
	for _, it := range v {
		if err, ok := it.(error); ok {
			format = fmt.Sprintf("err_type=%T ", err) + format
		}
	}
	l.log.Output(l.depth, fmt.Sprintf(format, v...))
}

// isTimestampDisabled allows to remove timestamps in logs if env variable
// LOG_DISABLE_TIMESTAMP is set
//
// Use case: a service is run under an external log collector that already appends
// timestamps, e.g. systemd supplying logs to journald
func isTimestampDisabled() bool { return os.Getenv("LOG_DISABLE_TIMESTAMP") != "" }

func inferLogFlags() int {
	if isTimestampDisabled() {
		return log.Llongfile | log.Lmsgprefix
	}
	return defaultLogFlags
}
