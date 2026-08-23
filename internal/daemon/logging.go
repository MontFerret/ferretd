package daemon

type logField string

const (
	logFieldEndpoint logField = "endpoint"
	logFieldVersion  logField = "version"
)

const (
	logMessageStarted        = "ferretd started"
	logMessageShutdownFailed = "ferretd shutdown failed"
)

func (f logField) FieldName() string {
	return string(f)
}
