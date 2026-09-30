package avelin

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func readAllEvents(t *testing.T, r io.Reader) ([]sseEvent, error) {
	t.Helper()
	sr := newSSEReader(r)
	var events []sseEvent
	for {
		ev, err := sr.next()
		if err != nil {
			return events, err
		}
		events = append(events, ev)
	}
}

func TestSSEReader(t *testing.T) {
	input := ": comment, ignored\n" +
		"event: first\n" +
		"data: line1\n" +
		"data:line2\n" +
		"data\n" +
		"\n" +
		"data: crlf\r\n\r\n" +
		"data: cr\r\r" +
		"event: no-data\n\n" +
		"data:  two spaces\n\n" +
		"id: 7\nretry: 100\nunknown: x\ndata: after ignored fields\n\n" +
		"data:\n\n" +
		"data: incomplete, discarded at EOF"
	want := []sseEvent{
		{event: "first", data: "line1\nline2\n"},
		{data: "crlf"},
		{data: "cr"},
		{data: " two spaces"},
		{data: "after ignored fields"},
		{data: ""},
	}
	for name, r := range map[string]io.Reader{
		"whole":    strings.NewReader(input),
		"one byte": iotest.OneByteReader(strings.NewReader(input)),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readAllEvents(t, r)
			if err != io.EOF {
				t.Fatalf("err = %v, want io.EOF", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("events:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestSSEReaderLongLine(t *testing.T) {
	long := strings.Repeat("x", 1<<20)
	got, err := readAllEvents(t, strings.NewReader("data: "+long+"\n\n"))
	if err != io.EOF || len(got) != 1 || got[0].data != long {
		t.Fatalf("got %d events, err %v", len(got), err)
	}
}

func TestSSEReaderReadError(t *testing.T) {
	boom := errors.New("boom")
	r := io.MultiReader(strings.NewReader("data: a\n\ndata: b"), iotest.ErrReader(boom))
	got, err := readAllEvents(t, r)
	if !errors.Is(err, boom) || len(got) != 1 {
		t.Fatalf("got %d events, err %v", len(got), err)
	}
}

func TestSSEReaderBOM(t *testing.T) {
	got, err := readAllEvents(t, strings.NewReader("\uFEFFdata: x\n\n"))
	if err != io.EOF || !reflect.DeepEqual(got, []sseEvent{{data: "x"}}) {
		t.Fatalf("got %q, err %v", got, err)
	}
}

// TestSSEReaderCRDispatchesAtOnce checks that an event ending in CR CR is
// delivered without waiting for more input.
func TestSSEReaderCRDispatchesAtOnce(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	go pw.Write([]byte("data: a\r\r"))
	got := make(chan sseEvent, 1)
	go func() {
		ev, _ := newSSEReader(pr).next()
		got <- ev
	}()
	select {
	case ev := <-got:
		if ev.data != "a" {
			t.Fatalf("event = %q", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event was not dispatched until more input arrived")
	}
}
