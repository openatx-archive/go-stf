package stf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMinicap(t *testing.T) {
	m := NewMinicap(dev)
	err := m.Start()
	assert.NoError(t, err)
	err = m.Stop()
	assert.NoError(t, err)
}
