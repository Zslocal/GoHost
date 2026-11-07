package server

import (
	"io"
	"net/http"
	"strings"

	"github.com/docker/docker/pkg/stdcopy"
)

type lineSink func(string) bool

func streamDockerLogs(w http.ResponseWriter, flusher http.Flusher, reader io.Reader, emit lineSink) error {
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(pw, pw, reader)
		pw.CloseWithError(err)
		done <- err
	}()

	buf := make([]byte, 8192)
	var partial strings.Builder
	for {
		n, err := pr.Read(buf)
		if n > 0 {
			partial.Write(buf[:n])
			chunk := partial.String()
			lines := strings.Split(chunk, "\n")
			partial.Reset()
			if !strings.HasSuffix(chunk, "\n") {
				partial.WriteString(lines[len(lines)-1])
				lines = lines[:len(lines)-1]
			}
			for _, line := range lines {
				if !emit(line) {
					pr.Close()
					return nil
				}
			}
		}
		if err != nil {
			if partial.Len() > 0 {
				emit(partial.String())
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
