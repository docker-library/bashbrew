package execpipe

import (
	"io"
	"os/exec"
)

// "io.ReadCloser" interface to a command's output where "Close()" is effectively "Wait()"
type Pipe struct {
	cmd *exec.Cmd
	out io.ReadCloser
}

// convenience wrapper for "New"
func NewCommand(cmd string, args ...string) (*Pipe, error) {
	return New(exec.Command(cmd, args...))
}

// start "cmd", capturing stdout in a pipe (be sure to call "Close" when finished reading to reap the process)
func New(cmd *exec.Cmd) (*Pipe, error) {
	p := &Pipe{
		cmd: cmd,
	}
	var err error
	if p.out, err = p.cmd.StdoutPipe(); err != nil {
		return nil, err
	}
	if err := p.cmd.Start(); err != nil {
		p.out.Close()
		return nil, err
	}
	return p, nil
}

func (pipe *Pipe) Read(p []byte) (n int, err error) {
	return pipe.out.Read(p)
}

func (p *Pipe) Close() error {
	return p.cmd.Wait()
}
