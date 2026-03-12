package driver

import (
	"context"
	"fmt"
	"net"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/shilucloud/csi-driver-hostpath-on-steriod/pkg/util"
	"google.golang.org/grpc"
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

	switch options.Mode {
	case ControllerMode:
		driver.controller = NewControllerService()
	case NodeMode:
		driver.node = NewNodeService()
	case AllMode:
		driver.controller = NewControllerService()
		driver.node = NewNodeService()
	default:
		return nil, fmt.Errorf("unknown mode: %s", options.Mode)
	}

	return &driver, nil
}

func (d *Driver) Run() error {

	scheme, addr, err := util.ParseEndpoint(d.options.Endpoint)
	if err != nil {
		return err
	}

	listenConfig := net.ListenConfig{}
	listener, err := listenConfig.Listen(context.Background(), scheme, addr)
	if err != nil {
		return err
	}
	fmt.Println(listener)

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

	return d.srv.Serve(listener)

}

func (d *Driver) Stop() {
	fmt.Print("Stopping the server")
}
