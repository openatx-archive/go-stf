package stf

import "image"

type Servicer interface {
	Start() error
	Stop() error
}

// type Device interface {
// Capture CaptureService
// // Touch TouchService
// // UIAuto UIAService
// // WebView WebViewService
// }

type ScreenReader interface {
	Servicer
	NextImage() (image.Image, error)
	LastImage() (image.Image, error)
	Subscribe() (chan image.Image, error)
}

type Toucher interface {
	Servicer
	Down(x, y int) error
	Move(x, y int) error
	Up() error
}

type UITester interface {
	Servicer
	Address() string
}

type RotationWatcher interface {
	Servicer
	Rotation() (int, error)
	Subscribe() chan int
	Unsubscribe(chan int)
}
