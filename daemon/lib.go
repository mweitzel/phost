package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mweitzel/phost/event_loop"
	"github.com/mweitzel/phost/util"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"time"
)

const (
	mutexKeyJobCreation = "job-creation"
)

type JobInAction struct {
	Cmd *exec.Cmd
}

type JobDefinition struct {
	CmdStr  string
	Args    []string
	Env     map[string]string
	ExecCmd *exec.Cmd
	ActionsUp
}

func (j *JobDefinition) Recover() {
	switch status := j.Status(); status {
	case "missing":
		fmt.Println("job missing")
		j.InitJob()
	case "running":
		// todo; check on it? its probably fine
	case "exited":
		// todo; restart policy
		j.InitJob()
		j.Dispatch(string(Display))
	default:
		panic("fix this: " + status)
	}
}

func (j *JobDefinition) Status() string {
	// hasn't started yet
	if j.ExecCmd == nil {
		return "missing"
	} else {
		// running, Handle and Handle.CmdStr are attached
		if j.ExecCmd.ProcessState == nil {
			return "running"
		}
		// if ProcessState exists it has exited
		return "exited"
	}
}

func (j *JobDefinition) InitJob() {
	cmd := exec.Command(j.CmdStr, j.Args...)
	j.ExecCmd = cmd
	// todo: manage stdout/stderr
	err := cmd.Start()

	// when cmd.Wait() completes, j.ExecCmd.ProcessState will be attached
	go func() { cmd.Wait(); j.Dispatch(string(Display)) }()

	if err != nil {
		fmt.Println("that's bad")
		fmt.Println(err)
	}
}

type Daemon struct {
	Ev ActionsUp
	//portAvailability map[int]bool
	Jobs               []*JobDefinition
	lastReport, report string
	shutdownMut        *sync.Mutex
	util.MultiMutex
}

func (d *Daemon) Exit(code int8) {
	d.Ev.Exit(code)
}

func (d *Daemon) AddListener(r event_loop.MatchStringer, cb func(ctx context.Context) error) (id int) {
	return d.Ev.AddListener(r, cb)
}

func (d *Daemon) RemoveListener(id int) {
	d.Ev.RemoveListener(id)
}

func (d *Daemon) Dispatch(event string) {
	if d.Ev != nil {
		d.Ev.Dispatch(event)
	} else {
		fmt.Println("config a dispatcher; event missed:", event)
	}
	//panic("implement me")
}

type ActionsUp interface {
	Dispatch(event string)
	AddListener(r event_loop.MatchStringer, cb func(ctx context.Context) error) (id int)
	RemoveListener(id int)
	Exit(code int8)
}

func New(cmdLines [][]string) *Daemon {
	d := &Daemon{
		shutdownMut: &sync.Mutex{},
		MultiMutex:  util.NewMultiMutex(),
	}
	jobs := d.JobSpec(cmdLines)
	d.Jobs = jobs
	return d
}

const (
	Display           = EventString("display")
	IntervalDisplay   = EventString("interval-display")
	IntervalKeepAlive = EventString("interval-keep-alive")
	IntervalSelfCheck = EventString("interval-self-check")
)

type EventString string

func (s EventString) MatchString(s2 string) bool {
	return string(s) == s2
}

func (d *Daemon) DisplayDebug() {
	d.lastReport = d.report
	type miniDef struct {
		Status string
		Id     int
		Cmd    string
		Args   []string
	}
	var xx []string
	xx = util.Map_tu(d.Jobs, func(j *JobDefinition) string {
		status := j.Status()

		view := miniDef{
			Status: status,
			Id:     0,
			Cmd:    j.CmdStr,
			Args:   j.Args,
		}

		switch status {
		case "missing":
			view.Id = -1
		case "running":
			view.Id = -2
			if j.ExecCmd != nil &&
				j.ExecCmd.Process != nil {
				view.Id = j.ExecCmd.Process.Pid
			}
		case "exited":
			if j.ExecCmd != nil &&
				j.ExecCmd.Process != nil {
				view.Id = j.ExecCmd.Process.Pid
			}
		default:
			panic("implement me: " + status)
		}

		s, err := json.Marshal(view)
		if err != nil {
			return err.Error()
		}
		return string(s)
	})

	d.report = strings.Join(xx, "\n")
	if d.report != d.lastReport {
		fmt.Println("pid", os.Getpid())
		fmt.Println(d.report)
	}
}

func (d *Daemon) JobSpec(cmdLines [][]string) (jobs []*JobDefinition) {
	for _, cmdLine := range cmdLines {
		cmd := cmdLine[0]
		args := cmdLine[1:]

		job := JobDefinition{
			CmdStr:    cmd,
			Args:      args,
			Env:       nil,
			ActionsUp: d,
		}
		jobs = append(jobs, &job)
	}
	return jobs
}

func (d *Daemon) FixMissing() {
	// don't start jobs while in the middle of shutdown
	d.LockOn(mutexKeyJobCreation, func() {
		for _, job := range d.Jobs {
			job.Recover()
		}
	})
}

func (d *Daemon) RegisterListeners() {
	d.AddListener(IntervalDisplay, func(ctx context.Context) error {
		d.DisplayDebug()
		time.Sleep(5000 * time.Millisecond)
		d.Ev.Dispatch(string(IntervalDisplay))
		return nil
	})

	d.AddListener(Display, func(ctx context.Context) error {
		d.DisplayDebug()
		time.Sleep(50 * time.Millisecond)
		d.DisplayDebug()
		return nil
	})

	d.AddListener(IntervalSelfCheck, func(ctx context.Context) error {
		d.FixMissing()
		time.Sleep(50 * time.Millisecond)
		d.Ev.Dispatch(string(IntervalSelfCheck))
		return nil
	})

	d.AddListener(IntervalKeepAlive, func(ctx context.Context) error {
		time.Sleep(200 * time.Millisecond)
		d.Ev.Dispatch(string(IntervalKeepAlive))
		return nil
	})

	d.AddListener(event_loop.Signal, func(ctx context.Context) error {
		// block job creation while shutting down
		d.LockOn(mutexKeyJobCreation, func() {
			d.shutdownMut.Lock()
			for i, job := range d.Jobs {
				if job.ExecCmd != nil &&
					job.ExecCmd.Process != nil {
					fmt.Println("killing", i, job.ExecCmd.Process.Pid)
					err := job.ExecCmd.Process.Kill()
					if err != nil &&
						err.Error() != "os: process already finished" {
						fmt.Println("trouble killing")
						fmt.Println(reflect.TypeOf(err))
						fmt.Println(err.Error())
					}
				}
			}
			time.Sleep(20 * time.Millisecond)
			d.Ev.(*event_loop.EventLoop).ExitBlocking(0)
		})
		return nil
	})

}
