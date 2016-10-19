package stf

import (
	"log"

	adb "github.com/openatx/go-adb"
)

var dev *adb.Device

func init() {
	// adbc, err := adb.New()
	adbc, err := adb.NewWithConfig(adb.ServerConfig{
		Host: "127.0.0.1",
	})
	// 	Host: "10.240.187.174",
	// 	Port: 5555,
	// })
	if err != nil {
		log.Fatal(err)
	}
	dev = adbc.Device(adb.AnyUsbDevice())
	log.Println(dev)
}
