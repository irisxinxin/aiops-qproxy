
package qcli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"time"
)

type Result struct {
	Stdout []byte
	Stderr []byte
	Err    error
}

type Options struct {
	Bin     string
	Workdir string
	Env     []string
	Timeout time.Duration
}

// Run pipes the given script to the q CLI.
func Run(script []byte, opt Options) Result {
	ctx := context.Background()
	if opt.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opt.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, opt.Bin)
	cmd.Dir = opt.Workdir
	// add NO_COLOR etc to reduce ANSI
	baseEnv := append(os.Environ(),
		"NO_COLOR=1",
		"CLICOLOR=0",
		"TERM=dumb",
	)
	cmd.Env = append(baseEnv, opt.Env...)
	var outB, errB bytes.Buffer
	cmd.Stdout = &outB
	cmd.Stderr = &errB
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{Err: err}
	}
	if err := cmd.Start(); err != nil {
		return Result{Err: err}
	}
	if _, err := stdin.Write(script); err != nil {
		return Result{Err: err}
	}
	_ = stdin.Close()
	err = cmd.Wait()
	return Result{Stdout: outB.Bytes(), Stderr: errB.Bytes(), Err: err}
}
