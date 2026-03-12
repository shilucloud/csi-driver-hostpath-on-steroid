package driver

import (
	"fmt"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

type NodeService struct {
}

func (ns *NodeService) NodeStageVolume(*csi.NodeStageVolumeRequest) *csi.NodeStageVolumeResponse {
	fmt.Print("this is nodestage vol")

	return nil
}

func (ns *NodeService) NodeUnStageVolume(*csi.NodeUnstageVolumeResponse) *csi.NodeUnstageVolumeResponse {
	fmt.Print("this is nodeUnstage vol")

	return nil
}

func (ns *NodeService) NodePublishVolume(*csi.NodePublishVolumeRequest) *csi.NodePublishVolumeResponse {
	fmt.Print("this iss nodepubvol")

	return nil
}

func (ns *NodeService) NodeUnpublishVolume(*csi.NodeUnpublishVolumeRequest) *csi.NodeUnpublishVolumeResponse {
	fmt.Print("this iss nodeunpubvol")

	return nil
}
