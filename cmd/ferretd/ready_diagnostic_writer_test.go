package main

import (
	"bytes"
	"sync"
)

type readyDiagnosticWriter struct {
	records chan []byte
	once    sync.Once
}

func newReadyDiagnosticWriter() *readyDiagnosticWriter {
	return &readyDiagnosticWriter{records: make(chan []byte, 1)}
}

func (w *readyDiagnosticWriter) Write(value []byte) (int, error) {
	copyOfValue := bytes.Clone(value)
	w.once.Do(func() {
		w.records <- copyOfValue
	})

	return len(value), nil
}
