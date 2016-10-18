package stf

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/facebookgo/freeport"
	adb "github.com/openatx/go-adb"
	"github.com/pkg/errors"
)

var (
	defaultMinicapFiles = []string{"/data/local/tmp/minicap", "/data/local/tmp/minicap.so"}
	ErrMinicapQuited    = errors.New("minicap quited")
)

type MinicapInfo struct {
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

type Minicap struct {
	*adb.Device

	width, height int
	rotation      int
	port          int
	stopped       bool
	quitC         chan bool
	once          sync.Once
	mu            sync.Mutex
	wg            sync.WaitGroup
}

func NewMinicap(d *adb.Device) *Minicap {
	return &Minicap{
		Device:  d,
		stopped: true,
	}
}

// Start twice is same as once
// If want to restart, you have to Stop() and then Start()
func (m *Minicap) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var err error
	m.once.Do(func() {
		// init variables
		m.stopped = false
		m.quitC = make(chan bool, 2)

		// push files and check environment
		mi, er := m.prepare()
		if er != nil {
			err = errors.Wrap(er, "prepare")
			return
		}
		m.width = mi.Width
		m.height = mi.Height
		m.rotation = mi.Rotation
		rC := make(chan int)
		m.wg.Add(2) // used for wait minicap process exit

		// run minicap and get images
		go m.runWithScreenRotate(rC)
		go m.keepPullScreenFromTcp()
	})
	return err
}

func (m *Minicap) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return errors.New("already stopped")
	}
	m.quitC <- true // quit minicap and keepRetriveScreen
	m.quitC <- true
	m.wg.Wait()
	m.once = sync.Once{} // reset sync.Once so we can call Start again
	m.stopped = true
	return nil
}

func (m *Minicap) Subscribe() (chan image.Image, error) {
	return nil, nil
}

func (m *Minicap) NextImage() (image.Image, error) {
	return nil, nil
}

func (m *Minicap) LastImage() (image.Image, error) {
	return nil, nil
}

func (m *Minicap) runWithScreenRotate(rotationChan chan int) error {
	defer m.wg.Done()
	m.killMinicap()
	errC := GoFunc(m.runMinicap)
	var restartFlag bool
	for {
		select {
		case err := <-errC:
			if !restartFlag {
				return err
			}
			restartFlag = false
			errC = GoFunc(m.runMinicap)
		case r := <-rotationChan:
			m.rotation = r
			m.killMinicap()
		case <-m.quitC:
			m.killMinicap()
			return nil
		}
	}
	return nil
}

func (m *Minicap) runMinicap() (err error) {
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

func (m *Minicap) killMinicap() error {
	return m.killProc("minicap", syscall.SIGKILL)
}

// FIXME(ssx): maybe need to put into go-adb
func (m *Minicap) killProc(psName string, sig syscall.Signal) (err error) {
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

// Check whether minicap is supported on the device
// Check adb forward
// For more information, see: https://github.com/openstf/minicap
func (m *Minicap) prepare() (mi MinicapInfo, err error) {
	if err = m.pushFiles(); err != nil {
		return
	}
	out, err := m.RunCommand("LD_LIBRARY_PATH=/data/local/tmp", "/data/local/tmp/minicap", "-i")
	if err != nil {
		return
	}
	err = json.Unmarshal([]byte(out), &mi)
	if err != nil {
		return
	}
	m.port, err = m.prepareForward()
	return
}

// adb forward tcp:{port} localabstract:minicap
func (m *Minicap) prepareForward() (port int, err error) {
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

func (m *Minicap) pushFiles() error {
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

func (m *Minicap) removeFiles() error {
	for _, filePath := range defaultMinicapFiles {
		_, err := m.RunCommand("rm", filePath) // some phone got no -f flag
		if err != nil {
			return err
		}
	}
	return nil
}

// retry with max 5
func (m *Minicap) keepPullScreenFromTcp() {
	defer m.wg.Done()
	retryLeft := 5
	for {
		startTime := time.Now()
		select {
		case <-GoFunc(m.pullScreenFromTcp):
			if time.Since(startTime) > time.Second*20 {
				retryLeft = 5 // reset retry
			}
			if retryLeft <= 0 {
				return
			}
			retryLeft -= 1
		case <-m.quitC:
			// no need to stop pullScreen
			// because when minicap quit, conn will read io.EOF
			return
		}

		// wait for the next start
		select {
		case <-time.After(time.Millisecond * 200):
		case <-m.quitC:
			return
		}
	}
}

func (m *Minicap) pullScreenFromTcp() error {
	conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(m.port))
	if err != nil {
		return err
	}
	defer conn.Close()
	rd := bufio.NewReader(conn)
	log.Println("Start to discard images")
	// TODO(ssx): get image
	io.Copy(ioutil.Discard, rd)
	return errors.New("minicap tcp stream closed")
}
