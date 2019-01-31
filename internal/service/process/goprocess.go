package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/gmbh-micro/defaults"
	"github.com/gmbh-micro/notify"
)

// GoProcess represents the controller for a Golang process
type GoProcess struct{}

// NewGoProcess returns a new golang process
func NewGoProcess(path, dir string) *Process {
	p := Process{
		Control: &GoProcess{},
		Info: info{
			// name: name,
			args: []string{},
			path: path,
			dir:  dir,
		},
		Runtime: runtime{
			running:     false,
			userKilled:  false,
			Pid:         -1,
			numRestarts: 0,
		},
		Errors: perr{
			errors: []error{},
		},
	}

	return &p
}

// Start a process
func (g *GoProcess) Start(p *Process) (int, error) {

	getPidChan := make(chan int, 1)
	go g.ForkExec(p, getPidChan)
	pid := <-getPidChan

	if pid != -1 {
		// p.log(fmt.Sprintf("proccess started pid=(%d)", pid))
		return pid, nil
	}
	return -1, errors.New("could not start process")
}

// Build a process
func (g *GoProcess) Build(p *Process) (int, error) {
	return 1, nil
}

// Restart a process
func (g *GoProcess) Restart(p *Process) (int, error) {
	return 1, nil
}

// ForkExec a process
func (g *GoProcess) ForkExec(p *Process, pid chan int) {
	cmd := g.GetCmd(p)
	listener := make(chan error)
	err := cmd.Start()
	if err != nil {
		fmt.Println(fmt.Sprintf("Could not start process (error=%v)", err))
		pid <- -1
		return
	}

	go func() {
		listener <- cmd.Wait()
	}()

	g.setRuntime(p, cmd.Process.Pid)
	pid <- cmd.Process.Pid

	select {
	case error := <-listener:
		if err != nil {
			// l.Message("proc error", "err: "+error.Error())
			p.Errors.errors = append(p.Errors.errors, error)
			// gprint.Err(fmt.Sprintf("Process Failed: %d", p.Runtime.Pid), 0)
		}

		if p.Runtime.userKilled {
			return
		}

		g.HandleFailure(p)
	}
}

// GetCmd of the process
func (g *GoProcess) GetCmd(p *Process) *exec.Cmd {
	var cmd *exec.Cmd
	cmd = exec.Command(p.Info.path, p.Info.args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	file, err := createLogFile(p.Info.dir+defaults.SERVICE_LOG_PATH, defaults.SERVICE_LOG_FILE)
	if err != nil {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd
	}
	cmd.Stdout = file
	cmd.Stderr = file
	return cmd
}

// HandleFailure -
func (g *GoProcess) HandleFailure(p *Process) {
	notify.StdMsgErr("fuck")
}

func (g *GoProcess) setRuntime(p *Process, pid int) {
	p.Runtime.running = true
	p.Runtime.StartTime = time.Now()
	p.Runtime.Pid = pid
}
