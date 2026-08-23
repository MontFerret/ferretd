// Package logging provides type-safe structured logging over zerolog.
package logging

import "github.com/rs/zerolog"

type (
	// Field identifies one component-owned structured logging field.
	Field interface {
		// FieldName returns the serialized field name.
		FieldName() string
	}

	// Value identifies one closed structured logging value.
	Value interface {
		// String returns the serialized enum value.
		String() string
	}

	// Logger wraps a zerolog logger without exposing raw string field methods.
	Logger struct {
		logger zerolog.Logger
	}

	// Context builds immutable logger context with typed fields.
	Context struct {
		context zerolog.Context
	}

	// Record builds one structured log record with typed fields.
	Record struct {
		event *zerolog.Event
	}
)

// New wraps a zerolog logger.
func New(logger zerolog.Logger) Logger {
	return Logger{logger: logger}
}

// Debug starts a debug record.
func (l Logger) Debug() Record {
	return Record{event: l.logger.Debug()}
}

// Info starts an info record.
func (l Logger) Info() Record {
	return Record{event: l.logger.Info()}
}

// Warn starts a warning record.
func (l Logger) Warn() Record {
	return Record{event: l.logger.Warn()}
}

// Error starts an error record.
func (l Logger) Error() Record {
	return Record{event: l.logger.Error()}
}

// With starts an immutable logger context.
func (l Logger) With() Context {
	return Context{context: l.logger.With()}
}

// String adds an open string value to the context.
func (c Context) String(field Field, value string) Context {
	c.context = c.context.Str(field.FieldName(), value)

	return c
}

// Enum adds a closed enum value to the context.
func (c Context) Enum(field Field, value Value) Context {
	return c.String(field, value.String())
}

// Logger completes the context and returns its logger.
func (c Context) Logger() Logger {
	return New(c.context.Logger())
}

// String adds an open string value to the record.
func (r Record) String(field Field, value string) Record {
	r.event = r.event.Str(field.FieldName(), value)

	return r
}

// Enum adds a closed enum value to the record.
func (r Record) Enum(field Field, value Value) Record {
	return r.String(field, value.String())
}

// Int adds an integer value to the record.
func (r Record) Int(field Field, value int) Record {
	r.event = r.event.Int(field.FieldName(), value)

	return r
}

// Bool adds a boolean value to the record.
func (r Record) Bool(field Field, value bool) Record {
	r.event = r.event.Bool(field.FieldName(), value)

	return r
}

// Err adds an error using zerolog's configured error field.
func (r Record) Err(err error) Record {
	r.event = r.event.Err(err)

	return r
}

// Msg completes and emits the record.
func (r Record) Msg(message string) {
	r.event.Msg(message)
}
