// RotationWatcher.apk Service
package stf

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	adb "github.com/openatx/go-adb"
	"github.com/openatx/go-adb/wire"
)

const (
	defaultRotationPkgName  = "jp.co.cyberagent.stf.rotationwatcher"
	defaultRotationMaxRetry = 3
)

type STFRotation struct {
	d           *adb.Device
	mu          sync.Mutex
	lastValue   int
	subscribers map[chan int]bool
	cmdConn     *wire.Conn
	wg          sync.WaitGroup
	stopped     bool
	leftRetry   int
}

func NewSTFRotation(d *adb.Device) *STFRotation {
	return &STFRotation{
		d:           d,
		subscribers: make(map[chan int]bool),
		leftRetry:   defaultRotationMaxRetry,
		lastValue:   -1,
	}
}

// 0, 90, 180, 270
func (s *STFRotation) Rotation() (int, error) {
	if s.lastValue == -1 || s.stopped {
		return 0, errors.New("Rotation not ready")
	}
	return s.lastValue, nil
}

func (s *STFRotation) Start() error {
	pmPath, err := s.preparePackage()
	if err != nil {
		return err
	}

	go func() {
		var ok = true
		for ok {
			s.wg.Add(1)
			err := s.consoleStartProcess(pmPath)
			if err == nil {
				s.leftRetry = defaultRotationMaxRetry
			} else {
				log.Printf("rotation run failed: %v, left retry %d", err, s.leftRetry)
			}

			s.mu.Lock()
			s.leftRetry -= 1
			if s.stopped || s.leftRetry <= 0 {
				for subC := range s.subscribers {
					s.Unsubscribe(subC)
				}
				ok = false
			}
			s.wg.Done()
			s.mu.Unlock()
		}
	}()
	return nil
}

func (s *STFRotation) Stop() error {
	// cancel retry and wait until stop
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	if s.cmdConn != nil {
		s.cmdConn.Close()
		s.cmdConn = nil
	}
	s.wg.Wait()
	return nil
}

func (s *STFRotation) Subscribe() chan int {
	s.mu.Lock()
	defer s.mu.Unlock()
	C := make(chan int, 1)
	s.subscribers[C] = true
	return C
}

// unsubscribe will also close channel
func (s *STFRotation) Unsubscribe(C chan int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subscribers, C)
	close(C)
}

func (s *STFRotation) pub(v int) {
	s.lastValue = v
	for subC := range s.subscribers {
		select {
		case subC <- v:
		case <-time.After(1 * time.Second):
			s.Unsubscribe(subC)
		}
	}
}

func (s *STFRotation) preparePackage() (pmPath string, err error) {
	if err := s.pushApk(); err != nil {
		return "", err
	}
	return s.getPackagePath(defaultRotationPkgName)
}

func (s *STFRotation) consoleStartProcess(pmPath string) error {
	fio, err := s.d.Command("CLASSPATH="+pmPath, "exec", "app_process", "/system/bin", defaultRotationPkgName+".RotationWatcher")
	if err != nil {
		return err
	}
	s.cmdConn = fio
	defer fio.Close()
	readCount := 0
	scanner := bufio.NewScanner(fio)
	for scanner.Scan() {
		val, err := strconv.Atoi(scanner.Text())
		if err != nil {
			return err
		}
		readCount += 1
		s.pub(val)
	}
	if readCount > 0 {
		return nil
	}
	return errors.New("Rotation got nothing")
}

func (s *STFRotation) pushApk() error {
	_, err := s.getPackagePath(defaultRotationPkgName) // If already installed, then skip
	if err == nil {
		return nil
	}
	phoneApkPath := "/data/local/tmp/RotationWatcher.apk"
	wc, err := s.d.OpenWrite(phoneApkPath, 0644, time.Now())
	if err != nil {
		return err
	}
	resp, err := http.Get("https://github.com/openatx/RotationWatcher.apk/releases/download/1.0/RotationWatcher.apk")
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("http download rotation watcher status " + resp.Status)
	}
	defer resp.Body.Close()
	log.Println("Downloading RotationWatcher.apk ...")
	if _, err = io.Copy(wc, resp.Body); err != nil {
		return err
	}
	log.Println("Done")
	if err := wc.Close(); err != nil {
		return err
	}
	_, err = s.checkCmdOutput("pm", "install", "-rt", phoneApkPath)
	return err
}

func (s *STFRotation) getPackagePath(name string) (path string, err error) {
	path, err = s.checkCmdOutput("pm", "path", name)
	if err != nil {
		return
	}
	if strings.HasPrefix(path, "package:") {
		path = strings.TrimSpace(path[len("package:"):])
		return
	}
	return "", errors.New("not rotationwatcher package found")
}

func (s *STFRotation) checkCmdOutput(name string, args ...string) (outStr string, err error) {
	args = append(args, ";", "echo", ":$?")
	outStr, err = s.d.RunCommand(name, args...)
	if err != nil {
		return
	}
	idx := strings.LastIndexByte(outStr, ':')
	if idx == -1 {
		return outStr, errors.New("adb shell error, parse exit code failed")
	}
	exitCode, _ := strconv.Atoi(strings.TrimSpace(outStr[idx+1:]))
	if exitCode != 0 {
		err = fmt.Errorf("[adb shell %s %s] exit code %d", name, strings.Join(args, " "), exitCode)
	}
	return outStr[0:idx], err
}
