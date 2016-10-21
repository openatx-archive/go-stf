package stf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTouch(t *testing.T) {
	touch := NewSTFTouch(dev)
	err := touch.Start()
	assert.NoError(t, err)
	touch.Down(0, 100, 330)
	touch.Up(0)
	err = touch.Stop()
	assert.NoError(t, err)
	err = touch.Wait()
	assert.NoError(t, err)
}
