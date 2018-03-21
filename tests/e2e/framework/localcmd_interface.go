package framework

import (
	"context"
	"io"
	"os/exec"
	"syscall"
)

type LocalCmd struct {
	ctx context.Context
}

func LocalExecutor(ctx context.Context) *LocalCmd {
	return &LocalCmd{ctx: ctx}
}

var _ Executor = &LocalCmd{}

func (l *LocalCmd) Run(stdin io.Reader, stdout, stderr io.Writer, command ...string) error {
	cmd := exec.CommandContext(l.ctx, command[0], command[1:]...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			if s, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return CommandError{ExitCode: s.ExitStatus()}
			}
		}
		return err
	}

	return nil
}

func (l *LocalCmd) Start(stdin io.Reader, stdout, stderr io.Writer, command ...string) (Command, error) {
	cmd := exec.CommandContext(l.ctx, command[0], command[1:]...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return localCommand{cmd: cmd}, nil
}

func (l *LocalCmd) Close() error {
	return nil
}

type localCommand struct {
	cmd *exec.Cmd
}

var _ Command = &localCommand{}

func (c localCommand) Wait() error {
	if err := c.cmd.Wait(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			if s, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return CommandError{ExitCode: s.ExitStatus()}
			}
		}
		return err
	}

	return nil
}

func (c localCommand) Kill() error {
	return c.cmd.Process.Kill()
}
