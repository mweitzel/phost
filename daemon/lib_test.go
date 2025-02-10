package daemon_test

import (
	"context"
	"fmt"
	"github.com/mweitzel/phost/daemon"
	"github.com/mweitzel/phost/event_loop"
	"github.com/mweitzel/phost/util"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func waitUntil(ev *event_loop.EventLoop, event event_loop.MatchStringer) *sync.WaitGroup {
	wg := &sync.WaitGroup{}
	wg.Add(1)
	var id int
	id = ev.AddListener(event, func(ctx context.Context) error {
		wg.Done()
		ev.RemoveListener(id)
		return nil
	})
	return wg
}

func TestUpdatesJobs(t *testing.T) {
	var contents = "echo hi there\necho bye now"
	var pContents = &contents

	d, ev := newDaemon(pContents)
	ev.Run()

	// twice in case the actual work one receives its event first
	waitUntil(ev, daemon.IntervalSelfCheck).Wait()
	waitUntil(ev, daemon.IntervalSelfCheck).Wait()

	jobsInitial := ""
	ev.LockOn("pause", func() {
		jobDesc := util.Map_tu(d.Jobs, func(j *daemon.JobDefinition) string {
			return fmt.Sprint(j.CmdStr, j.Args)
		})
		jobsInitial = strings.Join(jobDesc, "\n")
	})

	contents = "echo new stuff\necho bye now"

	waitUntil(ev, daemon.IntervalSelUpdateToNewJobDesc).Wait()
	waitUntil(ev, daemon.IntervalSelUpdateToNewJobDesc).Wait()

	jobsUpdated := " "
	ev.LockOn("pause", func() {
		jobDesc := util.Map_tu(d.Jobs, func(j *daemon.JobDefinition) string {
			return fmt.Sprint(j.CmdStr, j.Args)
		})
		jobsUpdated = strings.Join(jobDesc, "\n")
	})

	if jobsInitial == jobsUpdated {
		t.Errorf("jobs match but should not \n%v\n------------\n%v", jobsInitial, jobsUpdated)
		t.FailNow()
	}

	expectedDesc := strings.Join([]string{
		"echo[bye now]",   // original (second entry)
		"echo[new stuff]", // new job, appended
	}, "\n")
	if expectedDesc != jobsUpdated {
		t.Errorf("job does not match expected but should \n%v\n------------\n%v", expectedDesc, jobsUpdated)
		t.FailNow()
	}
}

func expect(t *testing.T, fn func() bool) {
	if fn() {
		return
	}

	_, file, line, _ := runtime.Caller(1)
	contents := util.Must(os.ReadFile(file))
	lines := strings.Split(string(contents), "\n")
	t.Error(fmt.Sprintf("failed at %s:%d\n*  %v", file, line, strings.Join(lines[line-1:line+3], "\n*  ")))

}

func newDaemon(str *string) (*daemon.Daemon, *event_loop.EventLoop) {
	d := daemon.New(func() ([][]string, error) {
		jobspec := parseConf(*str)
		return jobspec, nil
	})
	ev := event_loop.New()
	d.Ev = ev
	d.RegisterListeners()

	ev.Dispatch(string(daemon.IntervalSelfCheck))
	ev.Dispatch(string(daemon.IntervalKeepAlive))
	ev.Dispatch(string(daemon.IntervalDisplay))
	ev.Dispatch(string(daemon.IntervalSelUpdateToNewJobDesc))
	ev.Dispatch(string(daemon.Display))

	return d, ev
}

func parseConf(contents string) [][]string {
	blankMatcher := regexp.MustCompile("^\\s*$")
	lines := strings.Split(contents, "\n")
	lines = util.Map_tt(lines, func(s string) string {
		return strings.Split(s, "#")[0]
	})
	lines = util.Filter(lines, func(s string) bool {
		return !blankMatcher.MatchString(s)
	})
	var cmds [][]string
	cmds = util.Map_tu(lines, func(line string) []string {
		tokens := strings.Split(line, " ")
		tokens = util.Filter(tokens, func(s string) bool {
			return len(s) > 0
		})
		return tokens
	})
	return cmds
}
