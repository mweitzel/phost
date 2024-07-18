package util

import "sync"

func NewMultiMutex() MultiMutex {
	return MultiMutex{
		mutexes:     map[string]*sync.Mutex{},
		mutexesLock: &sync.Mutex{},
	}
}

type MultiMutex struct {
	mutexes     map[string]*sync.Mutex
	mutexesLock *sync.Mutex
}

func (l *MultiMutex) LockOn(name string, cb func()) {
	l.mutexesLock.Lock()
	mut, ok := l.mutexes[name]
	if !ok {
		mut = &sync.Mutex{}
		l.mutexes[name] = mut
	}
	l.mutexesLock.Unlock()

	mut.Lock()
	cb()
	mut.Unlock()
}
