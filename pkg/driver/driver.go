package driver

import (
	"context"
	"fmt"
	"net"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/shilucloud/csi-driver-hostpath-on-steriod/pkg/clientgo"
	"github.com/shilucloud/csi-driver-hostpath-on-steriod/pkg/util"
	"google.golang.org/grpc"
	klog "k8s.io/klog/v2"
)

type Driver struct {
	csi.UnimplementedIdentityServer
	controller    *ControllerService
	node          *NodeService
	driverName    string
	driverVersion string
	options       Options
	srv           *grpc.Server
	healthy       bool
}

type Options struct {
	Mode          Mode
	Endpoint      string
	Name          string
	driverVersion string
}

func NewDriver(options *Options) (*Driver, error) {
	driver := Driver{
		driverName:    options.Name,
		driverVersion: options.driverVersion,
		options:       *options,
	}

	goClient, err := clientgo.NewK8sClient()
	if err != nil {
		klog.ErrorS(err, "Error creating Kubernetes client")
		return nil, fmt.Errorf("error creating Kubernetes client: %w", err)
	}

	switch options.Mode {
	case ControllerMode:
		driver.controller = NewControllerService(goClient)
	case NodeMode:
		driver.node = NewNodeService()
	case AllMode:
		driver.controller = NewControllerService(goClient)
		driver.node = NewNodeService()
	default:
		klog.ErrorS(fmt.Errorf("Unknown mode"), "unknown mode provided", "mode", options.Mode)
		return nil, fmt.Errorf("unknown mode: %s", options.Mode)
	}

	klog.InfoS("Mode set to", "mode", options.Mode)

	return &driver, nil
}

func (d *Driver) Run() error {

	scheme, addr, err := util.ParseEndpoint(d.options.Endpoint)
	if err != nil {
		klog.ErrorS(err, "Error while parsing endpoint", "endpoint", d.options.Endpoint)
		return err
	}

	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(context.Background(), scheme, addr)
	if err != nil {
		klog.ErrorS(err, "Failed to listen", "scheme", scheme, "addr", addr)
		return err
	}
	klog.InfoS("Listening on", "listener", listener)

	d.srv = grpc.NewServer()
	csi.RegisterIdentityServer(d.srv, d)

	switch d.options.Mode {
	case ControllerMode:
		csi.RegisterControllerServer(d.srv, d.controller)
	case NodeMode:
		csi.RegisterNodeServer(d.srv, d.node)
	case AllMode:
		csi.RegisterControllerServer(d.srv, d.controller)
		csi.RegisterNodeServer(d.srv, d.node)
	}

	d.healthy = true

	klog.InfoS("Starting the server")

	return d.srv.Serve(listener)

}

func (d *Driver) Stop() {
	klog.InfoS("Stopping the server")
}
