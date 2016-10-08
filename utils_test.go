package stf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdbFileExists(t *testing.T) {
	exists := AdbFileExists(dev, "/system/bin/ls")
	assert.Equal(t, true, exists)
}

func TestAdbCheckOutput(t *testing.T) {
	outStr, err := AdbCheckOutput(dev, "echo", "hello")
	assert.NoError(t, err)
	assert.Equal(t, "hello\r\n", outStr)
}

func TestPushFileFromHTTP(t *testing.T) {
	err := PushFileFromHTTP(dev, "/data/local/tmp/tt.txt", 0644, "")
	assert.Error(t, err)
}
