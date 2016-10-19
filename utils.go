package stf

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	adb "github.com/openatx/go-adb"
)

func PushFileFromHTTP(d *adb.Device, dst string, perms os.FileMode, urlStr string) error {
	wc, err := d.OpenWrite(dst, perms, time.Now())
	if err != nil {
		return err
	}
	resp, err := http.Get(urlStr)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http download <%s> status %v", urlStr, resp.Status)
	}
	defer resp.Body.Close()
	log.Printf("Downloading to %s ...", dst)
	if _, err = io.Copy(wc, resp.Body); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return nil
}

func AdbCheckOutput(d *adb.Device, name string, args ...string) (outStr string, err error) {
	args = append(args, ";", "echo", ":$?")
	outStr, err = d.RunCommand(name, args...)
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

func AdbFileExists(d *adb.Device, path string) bool {
	_, err := AdbCheckOutput(d, "test", "-f", path)
	return err == nil
}

func GoFunc(f func() error) chan error {
	ch := make(chan error)
	go func() {
		ch <- f()
	}()
	return ch
}

type multiError struct {
	errs []error
}

func (m multiError) Error() string {
	var errStrs = make([]string, 0, len(m.errs))
	for _, err := range m.errs {
		errStrs = append(errStrs, err.Error())
	}
	return strings.Join(errStrs, "; ")
}

func wrapMultiError(errs ...error) error {
	merr := multiError{make([]error, 0)}
	for _, err := range errs {
		if err == nil {
			continue
		}
		merr.errs = append(merr.errs, err)
	}
	if len(merr.errs) == 0 {
		return nil
	}
	return merr
}
