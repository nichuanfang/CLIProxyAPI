package helps

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCodeBuddySSEReader(t *testing.T) {
	reader := NewCodeBuddySSEReader(strings.NewReader(": ping\r\nid: 1\r\nretry: 100\r\nevent: message\r\ndata: {\r\ndata: \"choices\":[]\r\ndata: }\r\n\r\nevent: error\n\ndata: [DONE]"))
	for _, want := range []CodeBuddySSEFrame{
		{Event: "message", Data: []byte("{\n\"choices\":[]\n}")},
		{Event: "error"},
		{Data: []byte("[DONE]")},
	} {
		frame, errRead := reader.ReadFrame()
		if errRead != nil || frame.Event != want.Event || string(frame.Data) != string(want.Data) {
			t.Fatalf("frame = %+v, error = %v, want %+v", frame, errRead, want)
		}
	}
	if _, errRead := reader.ReadFrame(); !errors.Is(errRead, io.EOF) {
		t.Fatalf("end error = %v, want EOF", errRead)
	}
}

func TestCodeBuddySSEReaderBareError(t *testing.T) {
	reader := NewCodeBuddySSEReader(strings.NewReader(`{"error":{"message":"rejected"}}`))
	frame, errRead := reader.ReadFrame()
	if errRead != nil || !frame.BareJSON || string(frame.Data) != `{"error":{"message":"rejected"}}` {
		t.Fatalf("frame = %+v, error = %v", frame, errRead)
	}
}

type codeBuddyBrokenReader struct{}

func (codeBuddyBrokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestCodeBuddySSEReaderDoesNotFlushBrokenFrame(t *testing.T) {
	reader := NewCodeBuddySSEReader(io.MultiReader(strings.NewReader("data: {\"choices\":[]}"), codeBuddyBrokenReader{}))
	if _, errRead := reader.ReadFrame(); !errors.Is(errRead, io.ErrUnexpectedEOF) {
		t.Fatalf("broken frame error = %v, want unexpected EOF", errRead)
	}
}
