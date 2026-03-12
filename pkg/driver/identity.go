package driver

import (
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

type IdentityService struct {
}

func (is *IdentityService) GetPluginCapabilities(*csi.GetPluginCapabilitiesRequest) *csi.GetPluginCapabilitiesResponse {
	fmt.Print("This is plugin cap")

	return nil
}

func (is *IdentityService) GetPluginInfo(*csi.GetPluginInfoRequest) *csi.GetPluginInfoResponse {
	fmt.Print("this is plugin info")
	return nil
}

func (is *IdentityService) Probe(*csi.ProbeRequest) *csi.ProbeResponse {
	fmt.Print("this is probe")
	return nil
}
