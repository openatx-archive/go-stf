package stf

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"

	adb "github.com/openatx/go-adb"
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
	closed        bool
	mu            sync.Mutex
}

func NewMinicap(d *adb.Device) ScreenReader {
	return &Minicap{
		Device: d,
		closed: true,
	}
}

func (m *Minicap) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.closed {
		return errors.New("minicap has not closed before start")
	}

	mi, err := m.prepare()
	if err != nil {
		return err
	}
	m.width = mi.Width
	m.height = mi.Height
	m.rotation = mi.Rotation
	rC := make(chan int)
	m.runWithScreenRotate(rC)
	return nil
}

func (m *Minicap) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.killMinicap()
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
	m.killMinicap()
	quit, err := m.runMinicap(m.rotation)
	if err != nil {
		return err
	}
	m.closed = false
	go func() {
		for !m.closed {
			select {
			case <-quit:
				m.closed = true
			case r := <-rotationChan:
				if m.rotation == r {
					continue
				}
				m.rotation = r
				m.killProc("minicap", syscall.SIGKILL)
				quit, err = m.runMinicap(m.rotation)
				if err != nil {
					m.closed = true
				}
			}
		}
	}()
	return nil
}

func (m *Minicap) runMinicap(rotation int) (quit chan bool, err error) {
	param := fmt.Sprintf("%dx%d@%dx%d/%d", m.width, m.height, m.width, m.height, rotation)
	c, err := m.Command("LD_LIBRARY_PATH=/data/local/tmp", "/data/local/tmp/minicap", "-P", param, "-S")
	if err != nil {
		return nil, err
	}
	quit = make(chan bool, 1)
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
		c.Close()
		return nil, errors.New("minicap starts failed, expect output: " + string(line))
	}
	go func() {
		for {
			_, _, er := buf.ReadLine()
			if er != nil {
				quit <- true
				c.Close()
				break
			}
		}
	}()
	return quit, nil
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
// For more information, see: https://github.com/openstf/minicap
func (m *Minicap) prepare() (mi MinicapInfo, err error) {
	if err = m.pushFiles(); err != nil {
		return
	}
	out, err := m.RunCommand("LD_LIBRARY_PATH=/data/local/tmp", "/data/local/tmp/minicap", "-i")
	if err != nil {
		return
	}
	// log.Println(out)
	err = json.Unmarshal([]byte(out), &mi)
	return
}

// adb forward tcp:{port} localabstract:minicap
// TODO
func (m *Minicap) prepareForward() error {
	return nil
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
