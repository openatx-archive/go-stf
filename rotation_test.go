package stf

import (
	"log"
	"testing"
	"time"

	adb "github.com/openatx/go-adb"
	"github.com/stretchr/testify/assert"
)

var dev *adb.Device

func init() {
	adbc, err := adb.New()
	// adbc, err := adb.NewWithConfig(adb.ServerConfig{
	// 	Host: "10.240.187.174",
	// 	Port: 5555,
	// })
	if err != nil {
		log.Fatal(err)
	}
	dev = adbc.Device(adb.AnyUsbDevice())
}

func TestRotation(t *testing.T) {
	assert := assert.New(t)

	r := NewSTFRotation(dev)
	subC := r.Subscribe()

	start := time.Now()
	err := r.Start()
	assert.Nil(err)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("start time used: %v", time.Since(start))
	select {
	case v := <-subC:
		t.Logf("read value: %d", v)
	case <-time.After(10 * time.Second):
		t.Fatal("get rotation timeout")
	}
	r.Unsubscribe(subC)
	time.Sleep(5 * time.Second)
	err = r.Stop()
	assert.Nil(err)
}

// func TestSleepLong(t *testing.T) {
// 	assert := assert.New(t)
// 	fio, err := dev.Command("sleep", "100")
// 	assert.Nil(err)
// 	t.Log(fio.Close())
// 	time.Sleep(5 * time.Second)
// }
