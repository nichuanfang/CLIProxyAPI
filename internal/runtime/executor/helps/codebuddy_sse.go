package helps

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
)

const codeBuddySSEMaxBytes = 52_428_800

// CodeBuddySSEFrame holds one complete upstream event. BareJSON identifies an
// unframed error body returned by an otherwise successful HTTP response.
type CodeBuddySSEFrame struct {
	Event    string
	Data     []byte
	BareJSON bool
}

// CodeBuddySSEReader assembles events before callers translate or observe usage.
// It does not impose network deadlines; cancellation belongs to the HTTP request.
type CodeBuddySSEReader struct {
	scanner *bufio.Scanner
}

func NewCodeBuddySSEReader(r io.Reader) *CodeBuddySSEReader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(nil, codeBuddySSEMaxBytes)
	return &CodeBuddySSEReader{scanner: scanner}
}

func (r *CodeBuddySSEReader) ReadFrame() (CodeBuddySSEFrame, error) {
	var frame CodeBuddySSEFrame
	var data [][]byte
	heldBytes := 0
	finish := func() CodeBuddySSEFrame {
		frame.Data = bytes.TrimSpace(bytes.Join(data, []byte("\n")))
		return frame
	}
	for r.scanner.Scan() {
		line := bytes.TrimSpace(r.scanner.Bytes())
		if len(line) == 0 {
			if len(data) > 0 || frame.Event != "" {
				return finish(), nil
			}
			continue
		}
		switch {
		case bytes.HasPrefix(line, []byte("data:")):
			value := bytes.TrimSpace(line[len("data:"):])
			heldBytes += len(value) + 1
			if heldBytes > codeBuddySSEMaxBytes {
				return frame, fmt.Errorf("codebuddy SSE event exceeds size limit")
			}
			data = append(data, bytes.Clone(value))
		case bytes.HasPrefix(line, []byte("event:")):
			frame.Event = strings.TrimSpace(string(line[len("event:"):]))
		case line[0] == '{' || line[0] == '[':
			return CodeBuddySSEFrame{Data: bytes.Clone(line), BareJSON: true}, nil
		}
	}
	if errScan := r.scanner.Err(); errScan != nil {
		return frame, errScan
	}
	if len(data) > 0 || frame.Event != "" {
		return finish(), nil
	}
	return frame, io.EOF
}
