package framework

import (
	"context"
	"io"
	"os/exec"
	"syscall"
)

type LocalCmd struct {
	ctx    context.Context
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

func LocalExecutor(ctx context.Context) *LocalCmd {
	return &LocalCmd{ctx: ctx}
}

func (l *LocalCmd) Exec(command []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	l.cmd = exec.CommandContext(l.ctx, command[0], command[1:]...)
	l.cmd.Stdin = stdin
	l.cmd.Stdout = stdout
	l.cmd.Stderr = stderr

	if err := l.cmd.Start(); err != nil {
		return 0, err
	}

	if err := l.cmd.Wait(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			if s, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return s.ExitStatus(), nil
			}
		}
		return 0, err
	}

	return 0, nil
}

func (l *LocalCmd) Close() error {
	return nil
}
