package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/shilucloud/csi-driver-hostpath-on-steriod/pkg/driver"
)

func main() {
	fmt.Println("This is a new go module")
	var (
		endpoint = flag.String("endpoint", "unix:///var/run/csi.sock", "Set the Unix Domain Socket Path")
		mode     = flag.String("mode", "controller", "Used to define whether this is controller component or node component")
		name     = flag.String("name", "csi.driver.hostpath.on.steriod", "Name of the CSI Driver")
	)
	flag.Parse()

	d, err := driver.NewDriver(&driver.Options{
		Mode:     driver.Mode(*mode),
		Endpoint: *endpoint,
		Name:     *name,
	})

	if err != nil {
		fmt.Print("There is been an error during intialization of driver %s", err)
		os.Exit(1)
	}

	if err := d.Run(); err != nil {
		fmt.Printf("Error %s, running the driver", err.Error())
	}

}
