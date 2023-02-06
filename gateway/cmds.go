package gateway

import (
	"log"
	"os"
	"os/exec"
	"strings"
)

type command struct {
	cmd  string
	args []string
}

func newCmd(cmd string, args ...string) *command {
	return &command{cmd, args}
}

func (c *command) run() error {
	return runSysCmd(c.cmd, c.args...)
}

type ignoreErrsRunnable struct {
	cmds []runnable
}

func ignoreErrs(cmds ...runnable) *ignoreErrsRunnable {
	return &ignoreErrsRunnable{cmds: cmds}
}

func (r *ignoreErrsRunnable) run() error {
	for _, cmd := range r.cmds {
		cmd.run() // nolint: errcheck
	}
	return nil
}

func runSysCmd(cmd string, args ...string) error {
	log.Printf("running command: %s %s", cmd, strings.Join(args, " "))
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

type runnable interface {
	run() error
}

func runMany(cmds ...runnable) error {
	for _, cmd := range cmds {
		if err := cmd.run(); err != nil {
			return err
		}
	}
	return nil
}
