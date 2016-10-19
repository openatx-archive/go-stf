package stf

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/facebookgo/freeport"
	adb "github.com/openatx/go-adb"
)

type minicapInfo struct {
	Id       int     `json:"id"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Xdpi     float32 `json:"xdpi"`
	Ydpi     float32 `json:"ydpi"`
	Size     float32 `json:"size"`
	Density  float32 `json:"density"`
	Fps      float32 `json:"fps"`
	Secure   bool    `json:"secure"`
	Rotation int     `json:"rotation"`
}

type minicapDaemon struct {
	width, height       int
	maxWidth, maxHeight int
	rotation            int
	port                int
	quitC               chan bool
	rotationC           chan int

	*adb.Device
	errorMixin
	safeMixin
}

func newMinicapDaemon(rotationC chan int, device *adb.Device) *minicapDaemon {
	if rotationC == nil {
		rotationC = make(chan int)
	}
	return &minicapDaemon{
		rotationC: rotationC,
		Device:    device,
		maxWidth:  720,
		maxHeight: 720,
	}
}

func (m *minicapDaemon) Start() error {
	return m.safeDo(ACTION_START,
		func() error {
			m.quitC = make(chan bool, 1)
			m.resetError()
			if err := m.pushFiles(); err != nil {
				return err
			}
			minfo, err := m.prepare()
			if err != nil {
				return err
			}
			m.width = minfo.Width
			m.height = minfo.Height
			m.rotation = minfo.Rotation
			go m.runScreenCaptureWithRotate() // TODO
			return nil
		})
}

func (m *minicapDaemon) Stop() error {
	return m.safeDo(ACTION_STOP,
		func() error {
			m.quitC <- true
			return m.Wait()
		})
}

// Check whether minicap is supported on the device
// Check adb forward
// For more information, see: https://github.com/openstf/minicap
func (m *minicapDaemon) prepare() (mi minicapInfo, err error) {
	if err = m.pushFiles(); err != nil {
		return
	}
	out, err := m.RunCommand("LD_LIBRARY_PATH=/data/local/tmp", "/data/local/tmp/minicap", "-i")
	if err != nil {
		return
	}
	err = json.Unmarshal([]byte(out), &mi)
	return
}

func (m *minicapDaemon) pushFiles() error {
	props, err := m.Properties()
	if err != nil {
		return err
	}
	abi, ok := props["ro.product.cpu.abi"]
	if !ok {
		return errors.New("No ro.product.cpu.abi propery")
	}
	sdk, ok := props["ro.build.version.sdk"]
	if !ok {
		return errors.New("No ro.build.version.sdk propery")
	}
	for _, filename := range []string{"minicap.so", "minicap"} {
		dst := "/data/local/tmp/" + filename
		if AdbFileExists(m.Device, dst) {
			continue
		}
		var urlStr string
		var perms os.FileMode = 0644
		if filename == "minicap.so" {
			urlStr = "https://github.com/openstf/stf/raw/master/vendor/minicap/shared/android-" + sdk + "/" + abi + "/minicap.so"
		} else {
			perms = 0755
			urlStr = "https://github.com/openstf/stf/raw/master/vendor/minicap/bin/" + abi + "/minicap"
		}
		err := PushFileFromHTTP(m.Device, dst, perms, urlStr)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *minicapDaemon) SetMaxWidthHeight(width int, height int) {
	m.maxWidth = width
	m.maxHeight = height
	m.rotationC <- m.rotation
}

func (m *minicapDaemon) runScreenCaptureWithRotate() {
	m.killMinicap()
	var err error
	defer m.doneError(err)
	errC := GoFunc(m.runScreenCapture)
	var needRestart bool
	for {
		select {
		case err = <-errC: // when normal exit, that is an error
			if !needRestart {
				return
			}
			needRestart = false
			err = nil
			errC = GoFunc(m.runScreenCapture)
		case r := <-m.rotationC:
			needRestart = true
			m.rotation = r
			m.killMinicap()
		case <-m.quitC:
			m.killMinicap()
			return
		}
	}
}

func (m *minicapDaemon) runScreenCapture() (err error) {
	if m.maxWidth <= 0 {
		m.maxWidth = m.width
	}
	if m.maxHeight <= 0 {
		m.maxHeight = m.height
	}
	param := fmt.Sprintf("%dx%d@%dx%d/%d", m.width, m.height, m.maxWidth, m.maxHeight, m.rotation)
	c, err := m.OpenCommand("LD_LIBRARY_PATH=/data/local/tmp", "/data/local/tmp/minicap", "-P", param, "-S")
	if err != nil {
		return
	}
	defer c.Close()
	buf := bufio.NewReader(c)

	// Example output below --.
	// PID: 9355
	// INFO: Using projection 720x1280@720x1280/0
	// INFO: (jni/minicap/JpgEncoder.cpp:64) Allocating 2766852 bytes for JPG encoder
	line, _, err := buf.ReadLine()
	if err != nil {
		return
	}
	if !strings.Contains(string(line), "PID:") {
		return errors.New("minicap starts failed, expect output: " + string(line))
	}
	for {
		_, _, err = buf.ReadLine()
		if err != nil {
			break
		}
	}
	return
}

func (m *minicapDaemon) killMinicap() error {
	return m.killProc("minicap", syscall.SIGKILL)
}

// FIXME(ssx): maybe need to put into go-adb
func (m *minicapDaemon) killProc(psName string, sig syscall.Signal) (err error) {
	out, err := m.RunCommand("ps", "-C", psName)
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
		m.RunCommand("kill", "-"+strconv.Itoa(int(sig)), pid)
	}
	return
}

type jpgTcpSucker struct {
	port  int
	conn  net.Conn
	quitC chan bool
	C     chan []byte

	errorMixin
	safeMixin
	*adb.Device
}

func (s *jpgTcpSucker) Start() error {
	return s.safeDo(ACTION_START, func() error {
		s.resetError()
		var err error
		s.C = make(chan []byte, 3)
		s.quitC = make(chan bool, 1)
		s.port, err = s.prepareForward()
		if err != nil {
			return err
		}
		go s.keepReadFromTcp()
		return nil
	})
}

func (s *jpgTcpSucker) Stop() error {
	return s.safeDo(ACTION_STOP, func() error {
		s.quitC <- true
		if s.conn != nil {
			s.conn.Close()
		}
		return s.Wait()
	})
}

// adb forward tcp:{port} localabstract:minicap
// TODO(ssx): make another service: CaptureTcpReadService
func (s *jpgTcpSucker) prepareForward() (port int, err error) {
	fws, err := s.ForwardList()
	if err != nil {
		return 0, err
	}
	// check if already forwarded
	for _, fw := range fws {
		if fw.Remote.Protocol == "localabstract" && fw.Remote.PortOrName == "minicap" {
			port, _ = strconv.Atoi(fw.Local.PortOrName)
			return
		}
	}
	port, err = freeport.Get()
	if err != nil {
		return
	}
	err = s.Forward(adb.ForwardSpec{"tcp", strconv.Itoa(port)}, adb.ForwardSpec{adb.FProtocolAbstract, "minicap"})
	return
}

type errorBinaryReader struct {
	rd  io.Reader
	err error
}

func (r *errorBinaryReader) ReadInto(datas ...interface{}) error {
	if r.err != nil {
		return r.err
	}
	for _, data := range datas {
		r.err = binary.Read(r.rd, binary.LittleEndian, data)
		if r.err != nil {
			return r.err
		}
	}
	return nil
}

// TODO(ssx): Do not add retry for now
func (s *jpgTcpSucker) keepReadFromTcp() (err error) {
	defer s.doneError(err)
	for {
		select {
		case err = <-GoFunc(s.readFromTcp):
		case <-s.quitC:
			return nil
		}
		select {
		case <-time.After(1000 * time.Millisecond):
		case <-s.quitC:
			return nil
		}
	}
}

func (s *jpgTcpSucker) readFromTcp() (err error) {
	conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(s.port))
	if err != nil {
		return
	}
	s.conn = conn
	defer conn.Close()

	var pid, rw, rh, vw, vh uint32
	var version, unused, orientation uint8

	rd := bufio.NewReader(conn)
	binRd := errorBinaryReader{rd: rd}
	err = binRd.ReadInto(&version, &unused, &pid, &rw, &rh, &vw, &vh, &orientation, &unused)
	if err != nil {
		return err
	}

	for {
		var size uint32
		if err = binRd.ReadInto(&size); err != nil {
			break
		}

		lr := &io.LimitedReader{rd, int64(size)}
		buf := bytes.NewBuffer(nil)
		_, err = io.Copy(buf, lr)
		if err != nil {
			break
		}
		if string(buf.Bytes()[:2]) != "\xff\xd8" {
			err = errors.New("jpeg format error, not starts with 0xff,0xd8")
			break
		}
		select {
		case s.C <- buf.Bytes(): // Maybe should use buffer instead
		default:
			// image should not wait or it will stuck here
		}
	}
	return err
}

type STFCapturer struct {
	*minicapDaemon
	*jpgTcpSucker
}

func NewSTFCapturer(device *adb.Device) *STFCapturer {
	return &STFCapturer{
		minicapDaemon: newMinicapDaemon(nil, device),
		jpgTcpSucker:  &jpgTcpSucker{Device: device},
	}
}

func (s *STFCapturer) Start() error {
	return wrapMultiError(
		s.minicapDaemon.Start(),
		s.jpgTcpSucker.Start())
}

func (s *STFCapturer) Stop() error {
	return wrapMultiError(
		s.minicapDaemon.Stop(),
		s.jpgTcpSucker.Stop())
}

func (s *STFCapturer) Wait() error {
	return wrapMultiError(
		s.minicapDaemon.Wait(),
		s.jpgTcpSucker.Wait())
}
