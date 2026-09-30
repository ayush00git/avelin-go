package avelin

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxSSELine = 8 << 20

var errStreamTruncated = fmt.Errorf("avelin: stream ended before completion: %w", io.ErrUnexpectedEOF)

// Stream iterates over a streamed response. It is not safe for concurrent
// use.
//
//	defer stream.Close()
//	for stream.Next() {
//		event := stream.Current()
//	}
//	if err := stream.Err(); err != nil {
//		// handle err
//	}
//
// A stream that ends without its terminating event ([DONE] for chat,
// message_stop for messages) reports an error wrapping io.ErrUnexpectedEOF.
// Streams are never retried once the response has started.
type Stream[T any] struct {
	body   io.ReadCloser
	events *sseReader
	decode func(Meta, sseEvent) (*T, bool, error)
	meta   Meta
	cur    T
	err    error
	done   bool
	closed bool
}

func newStream[T any](resp *http.Response, decode func(Meta, sseEvent) (*T, bool, error)) *Stream[T] {
	return &Stream[T]{
		body:   resp.Body,
		events: newSSEReader(resp.Body),
		decode: decode,
		meta:   Meta{StatusCode: resp.StatusCode, Header: resp.Header},
	}
}

// Next advances to the next event and reports whether there is one.
func (s *Stream[T]) Next() bool {
	if s.done || s.closed || s.err != nil {
		return false
	}
	for {
		ev, err := s.events.next()
		if err == io.EOF {
			err = errStreamTruncated
		} else if err != nil {
			err = fmt.Errorf("avelin: read stream: %w", err)
		}
		var v *T
		if err == nil {
			v, s.done, err = s.decode(s.meta, ev)
		}
		if err != nil {
			s.err = err
			s.Close()
			return false
		}
		if s.done {
			s.Close()
		}
		if v != nil {
			s.cur = *v
			return true
		}
		if s.done {
			return false
		}
	}
}

// Current returns the event read by the last successful call to Next.
func (s *Stream[T]) Current() T {
	return s.cur
}

// Err returns the error that stopped the stream, or nil if it completed or
// was closed by the caller.
func (s *Stream[T]) Err() error {
	return s.err
}

// Close releases the connection. It is safe to call more than once.
func (s *Stream[T]) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}

// Meta returns the HTTP status and headers of the stream response.
func (s *Stream[T]) Meta() Meta {
	return s.meta
}

type sseEvent struct {
	event string
	data  string
}

// sseReader parses text/event-stream as specified by the WHATWG HTML
// standard: comments, multi-line data fields and CR, LF or CRLF line endings.
// The id and retry fields are ignored.
type sseReader struct {
	scanner *bufio.Scanner
}

func newSSEReader(r io.Reader) *sseReader {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64<<10), maxSSELine)
	s.Split(scanSSELines)
	return &sseReader{scanner: s}
}

// next returns the next event that has data. At the end of the input it
// returns io.EOF, discarding an event not terminated by a blank line.
func (r *sseReader) next() (sseEvent, error) {
	var event string
	var data strings.Builder
	for r.scanner.Scan() {
		line := r.scanner.Text()
		if line == "" {
			if data.Len() > 0 {
				return sseEvent{event: event, data: strings.TrimSuffix(data.String(), "\n")}, nil
			}
			event = ""
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
	if err := r.scanner.Err(); err != nil {
		return sseEvent{}, err
	}
	return sseEvent{}, io.EOF
}

// scanSSELines is a bufio.SplitFunc for lines ending in CRLF, LF or CR.
func scanSSELines(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	i := bytes.IndexAny(data, "\r\n")
	switch {
	case i < 0 && atEOF:
		return len(data), data, nil
	case i < 0:
		return 0, nil, nil
	case data[i] == '\n':
		return i + 1, data[:i], nil
	case i+1 < len(data):
		if data[i+1] == '\n' {
			return i + 2, data[:i], nil
		}
		return i + 1, data[:i], nil
	case atEOF:
		return i + 1, data[:i], nil
	default:
		return 0, nil, nil // lone CR at the end of the buffer: need the next byte
	}
}
