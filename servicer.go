package stf

import (
	"errors"
	"sync"
)

var (
	ErrServiceAlreadyStarted = errors.New("Service already started")
	ErrServiceNotStarted     = errors.New("Service not started")
)

type Servicer interface {
	Start() error
	Stop() error
	Wait() error
}

type multiServ struct {
	ss []Servicer
}

func (m *multiServ) Start() error {
	for _, s := range m.ss {
		if err := s.Start(); err != nil {
			return err
		}
	}
	return nil
}

func (m *multiServ) Stop() error {
	var err error
	for _, s := range m.ss {
		if er := s.Stop(); er != nil {
			err = er
		}
	}
	return err
}

func (m *multiServ) Wait() error {
	errC := make(chan error, len(m.ss))
	for _, s := range m.ss {
		go func(s Servicer) {
			errC <- s.Wait()
		}(s)
	}
	return <-errC
}

// Combine servicers into one servicer
func MultiServicer(ss ...Servicer) Servicer {
	return &multiServ{ss}
}

// Mutex
const (
	ACTION_START = iota
	ACTION_STOP
)

type safeMixin struct {
	mu      sync.Mutex
	started bool
}

func (t *safeMixin) safeDo(action int, f func() error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started && action == ACTION_START {
		return ErrServiceAlreadyStarted
	}
	if !t.started && action == ACTION_STOP {
		return ErrServiceNotStarted
	}
	t.started = (action == ACTION_START)
	return f()
}

func (t *safeMixin) IsStarted() bool {
	return t.started
}

// Mutex retry
// type safeErrorMixin struct {
// 	safeMixin
// 	errorMixin
// 	errC      chan error
// 	maxRetry  int
// 	f         func() error
// 	startFunc func() error
// 	stopFunc  func() error
// }

// func (t *safeErrorMixin) safeRetryDo(maxRetry int, dur time.Duration,
// 	startFunc func() error, stopFunc func() error) error {
// 	return t.safeDo(action, func() error {
// 		t.maxRetry = maxRetry
// 		t.startFunc = startFunc
// 		t.stopFunc = stopFunc
// 		t.errC = make(chan error, 1)
// 		return t.doWithRetry()
// 	})
// }

// func (t *safeErrorMixin) doWithRetry() error {
// 	if err := t.startFunc(); err != nil {
// 		return err
// 	}
// 	go func() {
// 		leftRetry := t.maxRetry
// 		for leftRetry > 0 {
// 			startTime := time.Now()
// 			err := t.Wait()
// 			if err != nil {
// 				leftRetry -= 1
// 				if time.Since(startTime) > 20*time.Second {
// 					leftRetry = t.maxRetry
// 				}
// 				t.stopFunc()
// 				// t.Stop()
// 			}
// 		}
// 	}()
// }

// // if exit retry again
// func (t *safeErrorMixin) Wait() error {
// 	err := t.errorMixin.Wait()
// 	// errC := GoFunc(t.errorMixin.Wait)
// 	// select {
// 	// case <- errC:
// 	// }
// 	return err
// }

// func (t *mutexRetryMixin) Wait() error {
// }

// Mixin helper to easy write Servicer
type errorMixin struct {
	errC chan error
	once *sync.Once
	wg   *sync.WaitGroup
	err  error
}

// this func must be called before use other functions
func (e *errorMixin) resetError() {
	e.once = &sync.Once{}
	e.wg = &sync.WaitGroup{}
	e.wg.Add(1)
}

func (e *errorMixin) Wait() error {
	e.wg.Wait()
	return e.err
}

func (e *errorMixin) doneError(err error) {
	e.once.Do(func() {
		e.err = err
		e.wg.Done()
	})
}

func (e *errorMixin) doneNilError() {
	e.doneError(nil)
}
