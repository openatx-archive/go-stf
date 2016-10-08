package stf

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
