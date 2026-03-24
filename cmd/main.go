package main

import (
	"flag"
	"os"

	"github.com/shilucloud/csi-driver-hostpath-on-steriod/pkg/driver"
	klog "k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {

	var (
		endpoint = flag.String("endpoint", "unix:///var/run/csi.sock", "Set the Unix Domain Socket Path")
		mode     = flag.String("mode", "controller", "Used to define whether this is controller component or node component")
		name     = flag.String("name", "csi.driver.hostpath.on.steriod", "Name of the CSI Driver")
	)

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	flag.Parse()
	podNamespace := os.Getenv("POD_NAMESPACE")

	d, err := driver.NewDriver(&driver.Options{
		Mode:         driver.Mode(*mode),
		Endpoint:     *endpoint,
		Name:         *name,
		PodNamespace: podNamespace,
	})

	if err != nil {

		klog.ErrorS(err, "Error during driver initialization")
		os.Exit(1)
	}

	if err := d.Run(); err != nil {
		klog.ErrorS(err, "Error running the driver")
		os.Exit(1)
	}

}
