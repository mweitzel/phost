package event_loop

import (
	"context"
	"fmt"
	"github.com/mweitzel/phost/util"
	"os"
	"os/signal"
	"regexp"
	"sync"
	"syscall"
	"time"
)

func New() *EventLoop {
	el := &EventLoop{
		wg:         &sync.WaitGroup{},
		events:     []string{},
		work:       []func(ctx context.Context) error{},
		sysExit:    make(chan int, 1),
		eventMap:   map[int]registeredListener{},
		MultiMutex: util.NewMultiMutex(),
	}

	// wait for Run to be called
	el.waitForRun()
	return el
}

type EventLoop struct {
	wg                      *sync.WaitGroup
	events                  []string
	work                    []func(ctx context.Context) error
	workInProgressCount     int
	dispatchInProgressCount int
	sysExit                 chan int
	eventMap                map[int]registeredListener
	eventMapCount           int
	util.MultiMutex
}

func (l *EventLoop) Exit(code int8) {
	l.sysExit <- int(code)
}

func (l *EventLoop) ExitBlocking(code int8) {
	l.Exit(code)
	l.Wait()
}

func (l *EventLoop) AddListener(r MatchStringer, cb func(ctx context.Context) error) (id int) {
	l.LockOn("eventMapCount", func() {
		l.eventMapCount += 1
		id = l.eventMapCount
		l.LockOn("eventMap", func() {
			l.eventMap[id] = registeredListener{
				r:  r,
				cb: cb,
			}

		})
	})
	return
}

func (l *EventLoop) RemoveListener(id int) {
	l.LockOn("eventMap", func() {
		delete(l.eventMap, id)
	})
}

type registeredListener struct {
	r  MatchStringer
	cb func(context.Context) error
}

type MatchStringer interface {
	MatchString(s string) bool
}

func (l *EventLoop) Ax() {
	l.sysExit <- 10
}

func (l *EventLoop) Wait() {

	globalWgWaited := make(chan bool, 1)
	go func(cb func()) {
		l.wg.Wait()
		cb()
	}(func() {
		globalWgWaited <- true
	})

	select {
	case eCode := <-l.sysExit:
		fmt.Println("hit sysExit, code:", eCode)
		os.Exit(128 + eCode)
	case <-globalWgWaited:
		fmt.Println("global thing did thing")
	}
}

var Signal *regexp.Regexp

func init() {
	Signal = regexp.MustCompile("^signal-.*")
}

func (el *EventLoop) Run() {
	// +foo
	el.wg.Add(1)

	// important that Run's wg.add comes before this
	el.runWasCalledAndStarted()

	el.wg.Add(1)
	go func() {
		el.consumeEventsConsumeStack()
		el.wg.Done()
	}()

	go func() {
		el.handleSigs()
	}()

	// -foo
	el.wg.Done()
}

func (el *EventLoop) waitForRun() {
	// +bar
	el.wg.Add(1)
}
func (el *EventLoop) runWasCalledAndStarted() {
	// -bar
	el.wg.Done()
}

func (l *EventLoop) consumeEventsConsumeStack() {
	// there is a very off chance that dispatching an event as the absolute
	// last thing a unit of work does could miss the "didConsume" check
	// ..so we just go twice, it doesn't cost anything
	//twice := false
	for {
		l.doWork()

		wip := 0
		l.LockOn("workInProgressCount", func() {
			wip = l.workInProgressCount
		})

		dip := 0
		l.LockOn("dispatchInProgressCount", func() {
			dip = l.dispatchInProgressCount
		})

		didConsume := l.consumeEvent()
		if !didConsume && wip == 0 && dip == 0 {
			break
		}
		// this is the difference between maxing out a core and being practically idle
		time.Sleep(15 * time.Microsecond)
	}
}

func (l *EventLoop) doWork() {
	for {
		toBreak := false
		l.LockOn("work", func() {
			toBreak = len(l.work) == 0
		})
		if toBreak {
			break
		}

		l.LockOn("workInProgressCount", func() { l.workInProgressCount += 1 })

		var outerOneWork func(ctx context.Context) error
		l.LockOn("work", func() {
			oneWork, rest := firstRest(l.work)
			outerOneWork = oneWork
			l.work = rest
		})

		go func(oneWork func(ctx context.Context) error) {
			ctx := context.Background()
			err := oneWork(ctx)
			if err != nil {
				fmt.Println("work didn't work", err)
			}
			l.LockOn("workInProgressCount", func() { l.workInProgressCount -= 1 })
		}(outerOneWork)
	}
}

func (l *EventLoop) consumeEvent() bool {
	var out bool
	l.LockOn("events", func() {
		if len(l.events) == 0 {
			out = false
			return
		} else {
			out = true
		}

		event, rest := firstRest(l.events)
		l.events = rest

		jobs := l.findMatchingListeners(event)
		for _, job := range jobs {
			l.LockOn("work", func() {
				l.work = append(l.work, job.cb)
			})
		}

	})
	return out
}

func (l *EventLoop) findMatchingListeners(event string) (out []registeredListener) {
	l.LockOn("eventMap", func() {
		for _, listener := range l.eventMap {
			if listener.r.MatchString(event) {
				out = append(out, listener)
			}
		}
	})

	return out
}

func firstRest[T any](all []T) (first T, rest []T) {
	size := len(all)
	if size == 0 {
		return
	} else if size == 1 {
		first = all[0]
		return
	} else {
		first = all[0]
		rest = all[1:]
		return
	}
}

func (l *EventLoop) handleSigs() {
	sigs := make(chan os.Signal, 5)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	for {
		sig := <-sigs
		fmt.Println("Signal", sig.String(), "caught, forwarding to event loop")
		l.Dispatch("signal-" + sig.String())
	}
}

func (l *EventLoop) Dispatch(event string) {
	l.LockOn("dispatchInProgressCount", func() {
		l.dispatchInProgressCount += 1
	})
	go func() {
		l.LockOn("events", func() {
			l.events = append(l.events, event)
		})
		l.LockOn("dispatchInProgressCount", func() {
			l.dispatchInProgressCount -= 1
		})
	}()
}
