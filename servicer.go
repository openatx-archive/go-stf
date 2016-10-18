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

type threadSafeServ struct {
	mu      sync.Mutex
	started bool
	serv    Servicer
}

func (t *threadSafeServ) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return ErrServiceAlreadyStarted
	}
	return t.serv.Start()
}

func (t *threadSafeServ) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.started {
		return ErrServiceNotStarted
	}
	return t.Stop()
}

func (t *threadSafeServ) IsStarted() bool {
	return t.started
}

func (t *threadSafeServ) Wait() error {
	return t.serv.Wait()
}

func ThreadSafeServicer(s Servicer) Servicer {
	return &threadSafeServ{}
}

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

// type SleepService struct {
// 	cmd *exec.Cmd
// 	errorMixin
// }

// func (s *SleepService) Start() error {
// 	s.cmd = exec.Command("sleep", "10")
// 	go func() {
// 		s.writeError(s.cmd.Run())
// 	}()
// 	return nil
// }

// func (s *SleepService) Stop() error {
// 	defer s.doneNilError()
// 	if s.cmd != nil && s.cmd.Process != nil {
// 		return s.cmd.Process.Kill()
// 	}
// 	return nil
// }
