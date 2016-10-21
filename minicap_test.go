package stf

import (
	"bytes"
	"image/jpeg"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSTFCapturer(t *testing.T) {
	cap := NewSTFCapturer(dev, nil)
	err := cap.Start()
	assert.NoError(t, err)

	for i := 0; i < 20; i++ {
		select {
		case jpgData := <-cap.C:
			_, err := jpeg.Decode(bytes.NewReader(jpgData))
			assert.NoError(t, err)
		case <-time.After(time.Second * 2):
			t.Error("no image captured")
		}
	}

	err = cap.Stop()
	assert.NoError(t, err)
}
