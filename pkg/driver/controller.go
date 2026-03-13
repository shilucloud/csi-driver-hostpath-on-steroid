package driver

import (
	"context"
	"fmt"
	"strconv"

	"github.com/container-storage-interface/spec/lib/go/csi"
	hposv1 "github.com/shilucloud/csi-driver-hostpath-on-steriod/pkg/apis/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		return nil, status.Error(codes.InvalidArgument, "name must be provided")
	}

	byteSize := req.CapacityRange.GetRequiredBytes()
	if byteSize == 0 {
		byteSize = 1 * 1024 * 1024 * 1024
	}

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
		return nil, status.Error(codes.InvalidArgument, "no node found in topology")
	}

	fsType := req.Parameters["fsType"]
	if fsType == "" {
		fsType = "xfs"
	}

	volID := req.Name + "-" + node

	// idempotency check
	existingVol := &hposv1.HPOSVolume{}
	err := cs.goClient.Get(ctx, client.ObjectKey{Name: volID}, existingVol)
	if err == nil {
		// volume already exists — return stored values not request values
		klog.InfoS("volume already exists", "volID", volID)
		existingBytes, _ := strconv.ParseInt(existingVol.Spec.ByteSize, 10, 64)
		return &csi.CreateVolumeResponse{
			Volume: &csi.Volume{
				VolumeId:      existingVol.Spec.VolID,
				CapacityBytes: existingBytes,
				AccessibleTopology: []*csi.Topology{{
					Segments: map[string]string{
						"kubernetes.io/hostname": existingVol.Spec.NodeName,
					},
				}},
				VolumeContext: map[string]string{
					"fsType":   existingVol.Spec.FsType,
					"volID":    existingVol.Spec.VolID,
					"byteSize": existingVol.Spec.ByteSize,
				},
			},
		}, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, status.Errorf(codes.Internal, "failed to check existing volume: %v", err)
	}

	// create new CR
	vol := &hposv1.HPOSVolume{
		ObjectMeta: metav1.ObjectMeta{Name: volID},
		Spec: hposv1.HPOSVolumeSpec{
			VolID:    volID,
			NodeName: node,
			ByteSize: strconv.FormatInt(byteSize, 10),
			FsType:   fsType,
		},
	}
	if err = cs.goClient.Create(ctx, vol); err != nil {
		return nil, status.Errorf(codes.Internal, "error creating HPOSVolume: %v", err)
	}

	// update status separately
	vol.Status.Phase = "created"
	if err = cs.goClient.Status().Update(ctx, vol); err != nil {
		klog.ErrorS(err, "failed to update status", "volID", volID)
	}

	klog.InfoS("volume created", "volID", volID, "node", node, "byteSize", byteSize)

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
	klog.InfoS("Received ControllerPublishVolume request", "volID", req.VolumeId, "nodeID", req.NodeId)

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeId must be provided")
	}
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeId must be provided")
	}

	vol := &hposv1.HPOSVolume{}
	if err := cs.goClient.Get(ctx, client.ObjectKey{Name: req.VolumeId}, vol); err != nil {
		if apierrors.IsNotFound(err) {
			klog.ErrorS(err, "Volume not found", "volID", req.VolumeId)
			return nil, status.Errorf(codes.NotFound, "volume %s not found", req.VolumeId)
		}
		klog.ErrorS(err, "Failed to get volume", "volID", req.VolumeId)
		return nil, status.Errorf(codes.Internal, "failed to get volume: %v", err)
	}

	klog.InfoS("Found volume CR", "volID", req.VolumeId, "specNode", vol.Spec.NodeName, "phase", vol.Status.Phase)

	if vol.Spec.NodeName != req.NodeId {
		klog.ErrorS(nil, "Node mismatch", "volID", req.VolumeId, "specNode", vol.Spec.NodeName, "requestedNode", req.NodeId)
		return nil, status.Errorf(codes.InvalidArgument,
			"volume %s belongs to node %s, not %s",
			req.VolumeId, vol.Spec.NodeName, req.NodeId)
	}

	imgPath := fmt.Sprintf("/var/lib/hpos/%s.img", req.VolumeId)

	if vol.Status.Phase == "attached" {
		klog.InfoS("Volume already attached, returning idempotent response", "volID", req.VolumeId, "imgPath", imgPath)
		return &csi.ControllerPublishVolumeResponse{
			PublishContext: map[string]string{"imgPath": imgPath},
		}, nil
	}

	vol.Status.Phase = "attached"
	if err := cs.goClient.Status().Update(ctx, vol); err != nil {
		klog.ErrorS(err, "Failed to update volume status to attached", "volID", req.VolumeId)
		return nil, status.Errorf(codes.Internal, "failed to update status: %v", err)
	}

	klog.InfoS("Volume attached successfully", "volID", req.VolumeId, "node", req.NodeId, "imgPath", imgPath)
	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{"imgPath": imgPath},
	}, nil
}

func (cs *ControllerService) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	klog.InfoS("ControllerUnpublishVolume is called", "volID", req.VolumeId, "nodeID", req.NodeId)

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
