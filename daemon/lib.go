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
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	mutexKeyJobCreation = "job-creation"
)

type JobDefinition struct {
	CmdStr  string
	Args    []string
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
	go func() {
		cmd.Wait()
		j.Dispatch(string(Display))
	}()

	if err != nil {
		fmt.Println("that's bad")
		fmt.Println(err)
	}
}

type Daemon struct {
	Ev                 ActionsUp
	Jobs               []*JobDefinition
	AbandonedJobs      []*JobDefinition
	lastReport, report string
	shutdownMut        *sync.Mutex
	util.MultiMutex
	loadJobSpec         func() ([][]string, error)
	currentJobSpecLines [][]string
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

func New(loadJobSpec func() ([][]string, error)) *Daemon {
	d := &Daemon{
		shutdownMut: &sync.Mutex{},
		MultiMutex:  util.NewMultiMutex(),
	}
	d.loadJobSpec = loadJobSpec
	cmdLines := util.Must(loadJobSpec())
	d.currentJobSpecLines = cmdLines
	jobs := d.JobSpec(cmdLines)
	d.Jobs = jobs
	return d
}

const (
	Display                       = EventString("display")
	IntervalDisplay               = EventString("interval-display")
	IntervalKeepAlive             = EventString("interval-keep-alive")
	IntervalSelfCheck             = EventString("interval-self-check")
	IntervalSelUpdateToNewJobDesc = EventString("interval-self-update-to-new-job-desc")
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
	var makeReport = func(jobs []*JobDefinition) string {
		xx = util.Map_tu(jobs, func(j *JobDefinition) string {
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
		return strings.Join(xx, "\n")
	}
	clearedHeader := ""
	if len(d.AbandonedJobs) != 0 {
		clearedHeader = "====cleared jobs===="
	}
	report := fmt.Sprintf(
		"=====in progress====\n%v\n%s\n%v",
		makeReport(d.Jobs),
		clearedHeader,
		makeReport(d.AbandonedJobs),
	)
	d.report = report

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
		time.Sleep(150 * time.Millisecond)
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

	var stopJobBlocking = func(job *JobDefinition, extraDesc string) {
		if job.ExecCmd != nil &&
			job.ExecCmd.Process != nil {
			fmt.Println("killing", extraDesc, job.ExecCmd.Process.Pid)
			err := job.ExecCmd.Process.Kill()
			if err != nil &&
				err.Error() != "os: process already finished" {
				fmt.Println("trouble killing")
				fmt.Println(reflect.TypeOf(err))
				fmt.Println(err.Error())
			}
		}
	}

	var UpdateToNewJobDesc = func() {
		// don't start or mutate jobs when determining if we need to totally change what jobs are running
		d.LockOn(mutexKeyJobCreation, func() {
			newJobSpecLines, err := d.loadJobSpec()
			if err != nil {
				fmt.Println("Error updating jobspec!!")
				fmt.Println(err.Error())
				return
			}
			newJobSpec := d.JobSpec(newJobSpecLines)

			if !reflect.DeepEqual(newJobSpecLines, d.currentJobSpecLines) {
				d.currentJobSpecLines = newJobSpecLines
				oldJobSpec := d.Jobs
				toKeep, toRemove, toAdd := describeDiff(slices.Clone(newJobSpec), slices.Clone(oldJobSpec))
				fmt.Println("remove", toRemove)
				fmt.Println("add", util.Map_tu(toAdd, func(jd *JobDefinition) string {
					return fmt.Sprintf("%v %v", jd.CmdStr, jd.Args)
				}))

				d.AbandonedJobs = []*JobDefinition{}
				if len(toKeep) != len(newJobSpec) || len(toRemove) != 0 || len(toAdd) != 0 {
					for _, entry := range toRemove {
						toRemove := d.Jobs[entry.index]
						stopJobBlocking(toRemove, "(extra after conf load)")
						d.Jobs[entry.index] = nil
						d.AbandonedJobs = append(d.AbandonedJobs, toRemove)
					}

					d.Jobs = util.Filter(d.Jobs, func(job *JobDefinition) bool {
						return job != nil
					})

					for _, newJob := range toAdd {
						d.Jobs = append(d.Jobs, newJob)
					}
				}
				d.currentJobSpecLines = newJobSpecLines
			}
		})
	}

	d.AddListener(IntervalSelUpdateToNewJobDesc, func(ctx context.Context) error {
		UpdateToNewJobDesc()
		time.Sleep(150 * time.Millisecond)
		d.Ev.Dispatch(string(IntervalSelUpdateToNewJobDesc))
		return nil
	})

	d.AddListener(event_loop.Signal, func(ctx context.Context) error {
		// block job creation while shutting down
		d.LockOn(mutexKeyJobCreation, func() {
			d.shutdownMut.Lock()
			for i, job := range d.Jobs {
				stopJobBlocking(job, fmt.Sprintf("%v", i))
			}
			time.Sleep(20 * time.Millisecond)
			d.Ev.(*event_loop.EventLoop).ExitBlocking(0)
		})
		return nil
	})

}

type sliceEntry[T any] struct {
	index int
	t     T
}

type compare struct {
	Cmd  string
	Args []string
}

func describeDiff(newJobs, oldJobs []*JobDefinition) (toKeep []sliceEntry[compare], toRemove []sliceEntry[compare], toAdd []*JobDefinition) {
	for i, j := range oldJobs {
		found := -1
		for ii, jj := range newJobs {
			if jj != nil && reflect.DeepEqual(compare{
				Cmd:  j.CmdStr,
				Args: j.Args,
			}, compare{
				Cmd:  jj.CmdStr,
				Args: jj.Args,
			}) {
				found = ii
				break
			}
		}
		if found > -1 {
			newJobs[found] = nil
			toKeep = append(toKeep, sliceEntry[compare]{i, compare{
				Cmd:  j.CmdStr,
				Args: j.Args,
			}})
		} else {
			toRemove = append(toRemove, sliceEntry[compare]{i, compare{
				Cmd:  j.CmdStr,
				Args: j.Args,
			}})
		}
	}
	for _, j := range newJobs {
		if j != nil {
			toAdd = append(toAdd, j)
		}
	}
	return
}
