package lsp

import "os"

type stdioReadWriteCloser struct {
	reader *os.File
	writer *os.File
}

func newStdioReadWriteCloser() *stdioReadWriteCloser {
	return &stdioReadWriteCloser{reader: os.Stdin, writer: os.Stdout}
}

func (s *stdioReadWriteCloser) Read(buffer []byte) (int, error) {
	return s.reader.Read(buffer)
}

func (s *stdioReadWriteCloser) Write(buffer []byte) (int, error) {
	return s.writer.Write(buffer)
}

func (s *stdioReadWriteCloser) Close() error {
	return s.reader.Close()
}
