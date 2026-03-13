package driver

import (
	"context"
	"fmt"
	"strconv"

	"github.com/container-storage-interface/spec/lib/go/csi"
	hposv1 "github.com/shilucloud/csi-driver-hostpath-on-steriod/pkg/apis/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	// controllerCaps represents the capability of controller service.
	controllerCaps = []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
		csi.ControllerServiceCapability_RPC_MODIFY_VOLUME,
	}
)

type ControllerService struct {
	csi.UnimplementedControllerServer
	goClient client.Client
}

func (cs *ControllerService) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.InfoS("Received CreateVolume request", "request", req)

	if req.Name == "" {
		klog.ErrorS(fmt.Errorf("req.Name is empty"), "Name is not provided", "name", req.Name)
		return nil, status.Error(codes.InvalidArgument, "name must be provided")
	}

	byteSize := req.CapacityRange.GetRequiredBytes()
	if byteSize == 0 {
		byteSize = 1 * 1024 * 1024 * 1024 // default 1GB
	}

	// use preferred first, fall back to requisite
	var node string
	if req.AccessibilityRequirements != nil {
		if len(req.AccessibilityRequirements.Preferred) > 0 {
			node = req.AccessibilityRequirements.Preferred[0].Segments["kubernetes.io/hostname"]
		}
		if node == "" && len(req.AccessibilityRequirements.Requisite) > 0 {
			node = req.AccessibilityRequirements.Requisite[0].Segments["kubernetes.io/hostname"]
		}
	}
	if node == "" {
		klog.ErrorS(fmt.Errorf("node name is empty"), "no node found in topology", "node", node)
		return nil, status.Error(codes.InvalidArgument, "no node found in topology")
	}

	fsType := req.Parameters["fsType"]
	if fsType == "" {
		fsType = "xfs"
	}

	volID := req.Name + "-" + node
	klog.InfoS("Creating volume", "volID", volID, "node", node, "byteSize", byteSize, "fsType", fsType)

	// creating hopsvolume crd to represent the volume in Kubernetes
	vol := &hposv1.HPOSVolume{
		ObjectMeta: metav1.ObjectMeta{Name: volID},
		Spec: hposv1.HPOSVolumeSpec{
			VolID:    volID,
			NodeName: node,
			ByteSize: strconv.FormatInt(byteSize, 10),
			FsType:   fsType,
		},
		Status: hposv1.HPOSVolumeStatus{
			Phase: "created",
		},
	}
	err := cs.goClient.Create(ctx, vol)
	if err != nil {
		klog.ErrorS(err, "Error creating HPOSVolume CRD", "volID", volID)
		return nil, status.Error(codes.Internal, fmt.Sprintf("error creating HPOSVolume CRD: %v", err))
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      volID,
			CapacityBytes: byteSize,
			AccessibleTopology: []*csi.Topology{{
				Segments: map[string]string{
					"kubernetes.io/hostname": node,
				},
			}},
			VolumeContext: map[string]string{
				"fsType":   fsType,
				"volID":    volID,
				"byteSize": strconv.FormatInt(byteSize, 10),
			},
		},
	}, nil
}

func (cs *ControllerService) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.InfoS("Received DeleteVolume request", "request", req)
	volID := req.VolumeId

	klog.InfoS("Successfully Deleted the Volume", "volID", volID)

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *ControllerService) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	fmt.Print("This is controllerpubvol")
	return &csi.ControllerPublishVolumeResponse{}, nil
}

func (cs *ControllerService) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	fmt.Print("This is controllerunpubvol")
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *ControllerService) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, fmt.Errorf("ControllerExpandVolume not implemented")
}

func (cs *ControllerService) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, fmt.Errorf("ControllerGetVolume not implemented")
}

func (cs *ControllerService) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, fmt.Errorf("ListVolumes not implemented")
}

func (cs *ControllerService) ListVolumePages(ctx context.Context, req *csi.ListVolumesRequest, handler func(*csi.ListVolumesResponse) error) error {
	return fmt.Errorf("ListVolumePages not implemented")
}

func (cs *ControllerService) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, fmt.Errorf("GetCapacity not implemented")
}

func (cs *ControllerService) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {

	caps := make([]*csi.ControllerServiceCapability, 0, len(controllerCaps))
	for _, capability := range controllerCaps {
		c := &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: capability,
				},
			},
		}
		caps = append(caps, c)
	}
	return &csi.ControllerGetCapabilitiesResponse{Capabilities: caps}, nil

}

func (cs *ControllerService) ControllerModifyVolume(ctx context.Context, req *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {
	return nil, fmt.Errorf("ControllerModifyVolume not implemented")
}

func (cs *ControllerService) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, fmt.Errorf("CreateSnapshot not implemented")
}

func (cs *ControllerService) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, fmt.Errorf("DeleteSnapshot not implemented")
}

func (cs *ControllerService) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, fmt.Errorf("ListSnapshots not implemented")
}

func (cs *ControllerService) GetSnapshot(ctx context.Context, req *csi.GetSnapshotRequest) (*csi.GetSnapshotResponse, error) {
	return nil, fmt.Errorf("GetSnapshot not implemented")
}

func NewControllerService(goClient client.Client) *ControllerService {
	return &ControllerService{goClient: goClient}
}
