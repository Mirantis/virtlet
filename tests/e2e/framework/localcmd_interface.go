package framework

import (
	"context"
	"io"
	"os/exec"
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

	err := l.cmd.Start()
	if err != nil {
		return 1, err
	}
	return 0, nil
}

func (l *LocalCmd) Close() error {
	return nil
}
