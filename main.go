package main

import (
	"fmt"
	"github.com/mweitzel/phost/daemon"
	"github.com/mweitzel/phost/event_loop"
	"github.com/mweitzel/phost/util"
	"os"
	"path"
	"regexp"
	"strings"
)

func main() {
	jobspecPath := path.Join(util.Must(os.Getwd()), "phost-jobspec")

	if len(os.Args) == 2 {
		jobspecPath = os.Args[1]
	} else if len(os.Args) > 2 {
		usage()
		os.Exit(1)
	}

	jobspec := loadConf(jobspecPath)

	d := daemon.New(jobspec)
	ev := event_loop.New()
	d.Ev = ev
	d.RegisterListeners()

	ev.Dispatch(string(daemon.IntervalSelfCheck))
	ev.Dispatch(string(daemon.IntervalKeepAlive))
	ev.Dispatch(string(daemon.IntervalDisplay))
	ev.Dispatch(string(daemon.Display))

	go func() { ev.Run() }()
	ev.Wait()
}

func usage() {
	fmt.Println(strings.Join([]string{
		"Usage: ./phost [JOBSPEC]",
		"See readme for more information.",
	}, "\n"))
}

func loadConf(path string) [][]string {
	contents := util.Must(os.ReadFile(path))
	blankMatcher := regexp.MustCompile("^\\s*$")
	lines := strings.Split(string(contents), "\n")
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
