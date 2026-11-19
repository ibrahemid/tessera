package clipboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/ibrahemid/tessera/go/internal/procenv"
)

// toolTimeout bounds one tool run; a var so tests can shorten it.
var toolTimeout = 10 * time.Second

// pipeGrace keeps Wait from blocking on the stderr that forked wl-copy, xclip and xsel servers hold open.
const pipeGrace = 100 * time.Millisecond

const maxStderr = 4 << 10

// run executes one clipboard tool; a nil stdin or stdout is the null device.
func run(backend, op, path string, args []string, stdin io.Reader, stdout io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = procenv.Child()
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	stderr := &cappedBuffer{max: maxStderr}
	cmd.Stderr = stderr
	cmd.WaitDelay = pipeGrace
	err := cmd.Run()
	if err == nil || errors.Is(err, exec.ErrWaitDelay) {
		return nil
	}
	if ctx.Err() != nil {
		err = fmt.Errorf("no response within %s: %w", toolTimeout, ctx.Err())
	}
	return &OpError{Backend: backend, Op: op, Detail: firstLine(stderr.buf), Err: err}
}

type cappedBuffer struct {
	buf []byte
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.max - len(b.buf); room > 0 {
		b.buf = append(b.buf, p[:min(len(p), room)]...)
	}
	return len(p), nil
}

func firstLine(b []byte) string {
	for line := range bytes.Lines(b) {
		if s := string(bytes.TrimSpace(line)); s != "" {
			return s
		}
	}
	return ""
}
