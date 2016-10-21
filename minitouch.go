package stf

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	adb "github.com/openatx/go-adb"
	"github.com/pkg/errors"
)

type TouchAction int

const (
	TOUCH_DOWN = TouchAction(iota)
	TOUCH_MOVE
	TOUCH_UP
)

type STFTouch struct {
	cmdC       chan string
	conn       net.Conn
	maxX, maxY int
	rotation   int

	*adb.Device
	errorMixin
	safeMixin
}

func NewSTFTouch(device *adb.Device) *STFTouch {
	return &STFTouch{
		Device: device,
		cmdC:   make(chan string, 0),
	}
}

func (s *STFTouch) Start() error {
	return s.safeDo(_ACTION_START, func() error {
		s.resetError()
		if err := s.prepare(); err != nil {
			return err
		}
		go s.runBinary()
		go s.drainCmd()
		return nil
	})
}

func (s *STFTouch) Stop() error {
	return s.safeDo(_ACTION_STOP, func() error {
		s.killProc("minitouch", syscall.SIGKILL)
		return s.Wait()
	})
}

func (s *STFTouch) Down(index, posX, posY int) {
	s.cmdC <- fmt.Sprintf("d %v %v %v 50", index, posX, posY)
}

func (s *STFTouch) Move(index, posX, posY int) {
	s.cmdC <- fmt.Sprintf("m %v %v %v 50", index, posX, posY)
}

func (s *STFTouch) Up(index int) {
	s.cmdC <- fmt.Sprintf("u %d", index)
}

func (s *STFTouch) prepare() error {
	dst := "/data/local/tmp/minitouch"
	if AdbFileExists(s.Device, dst) {
		return nil
	}
	props, err := s.Properties()
	if err != nil {
		return err
	}
	abi, ok := props["ro.product.cpu.abi"]
	if !ok {
		return errors.New("No ro.product.cpu.abi propery")
	}
	urlStr := "https://github.com/openstf/stf/raw/master/vendor/minitouch/" + abi + "/minitouch"
	return PushFileFromHTTP(s.Device, dst, 0755, urlStr)
}

func (s *STFTouch) runBinary() (err error) {
	defer s.doneError(err)
	c, err := s.OpenCommand("/data/local/tmp/minitouch")
	if err != nil {
		return
	}
	defer c.Close()
	// _, err = io.Copy(ioutil.Discard, c)
	_, err = io.Copy(os.Stdout, c)
	return nil
}

func (s *STFTouch) drainCmd() {
	if err := s.dialWithRetry(); err != nil {
		s.doneError(errors.Wrap(err, "dial minitouch"))
		return
	}
	for c := range s.cmdC {
		c = strings.TrimSpace(c) + "\nc\n" // c: commit
		_, err := io.WriteString(s.conn, c)
		if err != nil {
			s.doneError(errors.Wrap(err, "write command to minitouch tcp"))
			s.conn.Close()
			s.conn = nil
			break
		}
	}
}

type lineFormatReader struct {
	bufrd *bufio.Reader
	err   error
}

func (r *lineFormatReader) Scanf(format string, args ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	var line []byte
	line, _, r.err = r.bufrd.ReadLine()
	if r.err != nil {
		return r.err
	}
	_, r.err = fmt.Sscanf(string(line), format, args...)
	return r.err
}

func (s *STFTouch) dialWithRetry() error {
	var err error
	for i := 0; i < 10; i++ {
		err = s.dialTouch()
		if err == nil {
			return nil
		}
		log.Println("dial minitouch service fail, reconnect, err is", err)
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

func (s *STFTouch) dialTouch() error {
	port, err := s.ForwardToFreePort(adb.ForwardSpec{adb.FProtocolAbstract, "minitouch"})
	if err != nil {
		return err
	}
	s.conn, err = net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	lineRd := lineFormatReader{bufrd: bufio.NewReader(s.conn)}
	var flag string
	var ver int
	var maxContacts, maxPressure int
	var pid int
	lineRd.Scanf("%s %d", &flag, &ver)
	lineRd.Scanf("%s %d %d %d %d", &flag, &maxContacts, &s.maxX, &s.maxY, &maxPressure)
	if err := lineRd.Scanf("%s %d", &flag, &pid); err != nil {
		s.conn.Close()
		return err
	}
	return nil
}

// FIXME(ssx): maybe need to put into go-adb
func (s *STFTouch) killProc(psName string, sig syscall.Signal) (err error) {
	out, err := s.RunCommand("ps", "-C", psName)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) <= 1 {
		return errors.New("No process named " + psName + " founded.")
	}
	var pidIndex int
	for idx, val := range strings.Fields(lines[0]) {
		if val == "PID" {
			pidIndex = idx
			break
		}
	}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if !strings.Contains(line, psName) {
			continue
		}
		pid := fields[pidIndex]
		s.RunCommand("kill", "-"+strconv.Itoa(int(sig)), pid)
	}
	return
}
