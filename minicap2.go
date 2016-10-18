package stf

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/facebookgo/freeport"
	adb "github.com/openatx/go-adb"
)

type STFCapturer struct {
	width, height int
	rotation      int
	port          int
	quitC         chan bool
	rotationC     chan int

	*adb.Device
	errorMixin
	mutexMixin
}

func NewSTFCapturer(rotationC chan int, device *adb.Device) Servicer {
	return &STFCapturer{
		Device: device,
	}
}

func (m *STFCapturer) Start() error {
	return m.safeDo(mutexActionStart,
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
			m.runScreenCaptureWithRotate() // TODO
			return nil
		})
}

func (m *STFCapturer) Stop() error {
	return m.safeDo(mutexActionStop,
		func() error {
			m.quitC <- true
			return m.Wait()
		})
}

// Check whether minicap is supported on the device
// Check adb forward
// For more information, see: https://github.com/openstf/minicap
func (m *STFCapturer) prepare() (mi MinicapInfo, err error) {
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

// adb forward tcp:{port} localabstract:minicap
// TODO(ssx): make another service: CaptureTcpReadService
func (m *STFCapturer) prepareForward() (port int, err error) {
	fws, err := m.ForwardList()
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
	err = m.Forward(adb.ForwardSpec{"tcp", strconv.Itoa(port)}, adb.ForwardSpec{adb.FProtocolAbstract, "minicap"})
	return
}

func (m *STFCapturer) pushFiles() error {
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

func (m *STFCapturer) runScreenCaptureWithRotate() {
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

func (m *STFCapturer) runScreenCapture() (err error) {
	param := fmt.Sprintf("%dx%d@%dx%d/%d", m.width, m.height, m.width, m.height, m.rotation)
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

func (m *STFCapturer) killMinicap() error {
	return m.killProc("minicap", syscall.SIGKILL)
}

// FIXME(ssx): maybe need to put into go-adb
func (m *STFCapturer) killProc(psName string, sig syscall.Signal) (err error) {
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
