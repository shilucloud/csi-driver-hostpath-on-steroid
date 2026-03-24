package driver

import (
	"context"
	"fmt"
	"strconv"

	"github.com/container-storage-interface/spec/lib/go/csi"
	hposv1 "github.com/shilucloud/csi-driver-hostpath-on-steriod/pkg/apis/v1"
	"github.com/shilucloud/csi-driver-hostpath-on-steriod/pkg/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	goClient  client.Client
	namespace string
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
		ObjectMeta: metav1.ObjectMeta{
			Name: volID,
			Finalizers: []string{
				"hposvolume.k8s.io.need-deletion"},
		},
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
	klog.InfoS("Received DeleteVolume request", "volID", req.VolumeId)

	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeId must be provided")
	}

	vol := &hposv1.HPOSVolume{}
	if err := cs.goClient.Get(ctx, client.ObjectKey{Name: req.VolumeId}, vol); err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("Volume already deleted, idempotent return", "volID", req.VolumeId)
			return &csi.DeleteVolumeResponse{}, nil
		}
		klog.ErrorS(err, "Failed to get volume", "volID", req.VolumeId)
		return nil, status.Errorf(codes.Internal, "failed to delete volume, internal error: %v", err)
	}

	nodeName := vol.Spec.NodeName
	imgPath := fmt.Sprintf("/var/lib/hpos/%s.img", req.VolumeId)
	jobName := "cleanup-" + req.VolumeId[:16] // truncate for k8s name limit

	klog.InfoS("Creating cleanup job to delete image file on node", "node", nodeName, "imgPath", imgPath)

	// create cleanup job on the target node
	job := util.DeleteImageJob(jobName, cs.namespace, nodeName, imgPath)

	if err := cs.goClient.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		klog.ErrorS(fmt.Errorf("failed to create cleanup job"), "Failed to create cleanup job", "job", jobName, "node", nodeName, "imgPath", imgPath, "error", err)
		return nil, status.Errorf(codes.Internal, "failed to create cleanup job: %v", err)
	}
	klog.InfoS("Cleanup job created", "job", jobName, "node", nodeName, "imgPath", imgPath)

	// Wait for the job to complete
	if err := util.WaitForJobCompletion(ctx, cs.goClient, cs.namespace, jobName); err != nil {
		klog.ErrorS(err, "Cleanup job failed", "job", jobName)
		return nil, status.Errorf(codes.Internal, "cleanup job failed: %v", err)
	}

	klog.InfoS("Cleanup job completed successfully", "job", jobName)

	isFinalizerExist, err := util.HasFinalizer(ctx, cs.goClient, req.VolumeId)
	if err != nil {
		klog.ErrorS(err, "Failed to check finalizer for HPOSVolume", "volID", req.VolumeId)
		return nil, status.Error(codes.Internal, "Failure while getting finalizer")
	}

	if isFinalizerExist {
		vol := &hposv1.HPOSVolume{}
		err := cs.goClient.Get(ctx, client.ObjectKey{Name: req.VolumeId}, vol)
		if err != nil {
			klog.ErrorS(err, "Failed to delete the finalizer for HPOSVolume", "volID", req.VolumeId)
			return nil, status.Error(codes.Internal, "failed to delete the finalizer for CR")
		}

		vol.Finalizers = []string{}
		err = cs.goClient.Update(ctx, vol)
		if err != nil {
			klog.ErrorS(err, "Failed to delete the finalizer for HPOSVolume", "volID", req.VolumeId)
			return nil, status.Error(codes.Internal, "failed to delete the finalizer for CR")
		}
		klog.InfoS("Removed the Finalizer successfully from HPOSVolume", "volID", req.VolumeId)
	}

	// delete the CR
	if err := cs.goClient.Delete(ctx, vol); err != nil {
		klog.ErrorS(err, "Failed to delete volume CR", "volID", req.VolumeId)
		return nil, status.Errorf(codes.Internal, "failed to delete volume CR: %v", err)
	}

	klog.InfoS("Successfully deleted volume", "volID", req.VolumeId)
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
			return nil, status.Errorf(codes.NotFound, "volume %s not found", req.VolumeId)
		}
		return nil, status.Errorf(codes.Internal, "failed to get volume: %v", err)
	}

	klog.InfoS("Found volume CR", "volID", req.VolumeId, "specNode", vol.Spec.NodeName, "phase", vol.Status.Phase)

	imgPath := fmt.Sprintf("/var/lib/hpos/%s.img", req.VolumeId)

	// idempotency — already attached to this node
	if vol.Status.Phase == "attached" && vol.Status.AttachedNode == req.NodeId {
		klog.InfoS("Volume already attached to requested node, idempotent return", "volID", req.VolumeId)
		return &csi.ControllerPublishVolumeResponse{
			PublishContext: map[string]string{"imgPath": imgPath},
		}, nil
	}

	// attached to a DIFFERENT node — error
	if vol.Status.Phase == "attached" && vol.Status.AttachedNode != req.NodeId {
		return nil, status.Errorf(codes.FailedPrecondition,
			"volume %s already attached to node %s",
			req.VolumeId, vol.Status.AttachedNode)
	}

	// node mismatch — volume belongs to different node
	if vol.Spec.NodeName != req.NodeId {
		return nil, status.Errorf(codes.InvalidArgument,
			"volume %s belongs to node %s, not %s",
			req.VolumeId, vol.Spec.NodeName, req.NodeId)
	}

	vol.Status.Phase = "attached"
	vol.Status.AttachedNode = req.NodeId
	if err := cs.goClient.Status().Update(ctx, vol); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update status: %v", err)
	}

	klog.InfoS("Volume attached successfully", "volID", req.VolumeId, "node", req.NodeId)
	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{"imgPath": imgPath},
	}, nil
}

func (cs *ControllerService) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	klog.InfoS("ControllerUnpublishVolume is called", "volID", req.VolumeId, "nodeID", req.NodeId)

	vol := &hposv1.HPOSVolume{}
	if err := cs.goClient.Get(ctx, client.ObjectKey{Name: req.VolumeId}, vol); err != nil {
		if apierrors.IsNotFound(err) {
			klog.InfoS("Volume not found, idempotent return",
				"volID", req.VolumeId)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		klog.ErrorS(err, "Failed to get volume", "volID", req.VolumeId)
		return nil, status.Errorf(codes.Internal, "failed to get volume: %v", err)
	}
	vol.Status.Phase = "detached"
	vol.Status.AttachedNode = ""

	if err := cs.goClient.Status().Update(ctx, vol); err != nil {
		klog.ErrorS(err, "Failed to update volume status to detached and attachednode to empty", "volID", req.VolumeId)
		return nil, status.Errorf(codes.Internal, "failed to update status and attachednode: %v", err)
	}

	klog.InfoS("Detached the Volume Successfully", "volID", req.VolumeId)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *ControllerService) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, fmt.Errorf("ControllerExpandVolume not implemented")
}

func (cs *ControllerService) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, fmt.Errorf("ControllerGetVolume not implemented")
}

func (cs *ControllerService) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	volumeList := &hposv1.HPOSVolumeList{}
	if err := cs.goClient.List(ctx, volumeList); err != nil {
		return nil, status.Errorf(codes.Internal,
			"failed to list volumes: %v", err)
	}

	// basic pagination support
	items := volumeList.Items
	if req.MaxEntries > 0 && int(req.MaxEntries) < len(items) {
		items = items[:req.MaxEntries]
	}

	volumes := make([]*csi.ListVolumesResponse_Entry, 0, len(items))
	for _, vol := range items {
		byteSize, _ := strconv.ParseInt(vol.Spec.ByteSize, 10, 64)

		var publishedNodes []string
		if vol.Status.AttachedNode != "" {
			publishedNodes = []string{vol.Status.AttachedNode}
		}

		volumes = append(volumes, &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      vol.Spec.VolID,
				CapacityBytes: byteSize,
			},
			Status: &csi.ListVolumesResponse_VolumeStatus{
				PublishedNodeIds: publishedNodes,
			},
		})
	}

	return &csi.ListVolumesResponse{
		Entries: volumes,
	}, nil
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
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name must be provided")
	}

	if req.SourceVolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "source volume ID must be provided")
	}

	vol := &hposv1.HPOSVolume{}
	err := cs.goClient.Get(ctx, client.ObjectKey{Name: req.SourceVolumeId}, vol)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "source volume %s not found", req.SourceVolumeId)
		}
		klog.ErrorS(err, "Failed to retrieve source volume", "sourceVolumeId", req.SourceVolumeId)
		return nil, status.Errorf(codes.Internal, "failed to retrieve source volume: %v", err)
	}

	nodeName := vol.Spec.NodeName
	namespace := cs.namespace

	klog.InfoS("NodeName and namespace", nodeName, namespace)

	// idempotency check
	existingSnapshot := &hposv1.HPOSSnapshot{}
	snapshotName := req.Name + "-" + nodeName
	err = cs.goClient.Get(ctx, client.ObjectKey{Name: snapshotName}, existingSnapshot)
	if err == nil {
		klog.InfoS("Snapshot already exists, returning existing snapshot", "snapshotName", snapshotName)
		return &csi.CreateSnapshotResponse{
			Snapshot: &csi.Snapshot{
				SnapshotId:     existingSnapshot.Spec.SnapshotID,
				SourceVolumeId: existingSnapshot.Spec.SourceVolID,
			},
		}, nil
	}
	if !apierrors.IsNotFound(err) {
		klog.ErrorS(err, "Failed to check for existing snapshot", "snapshotName", snapshotName)
		return nil, status.Errorf(codes.Internal, "failed to check for existing snapshot: %v", err)
	}
	klog.InfoS("Snapshot does not exist, proceeding with creation", "snapshotName", snapshotName)

	// creating the job to create snapshot on the target node
	jobName := "create-snapshot-" + req.SourceVolumeId[:16] // truncate for k8s name limit
	imgPath := fmt.Sprintf("/var/lib/hpos/%s.img", req.SourceVolumeId)
	snapshotPath := fmt.Sprintf("/var/lib/hpos/snapshots/%s", req.Name)
	job := util.CreateSnapshotJob(jobName, namespace, nodeName, imgPath, snapshotPath)

	if err := cs.goClient.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		klog.ErrorS(fmt.Errorf("failed to create Snapshot job"), "Failed to create Snapshot job", "job", jobName, "node", nodeName, "imgPath", imgPath, "snapshotPath", snapshotPath, "error", err)
		return nil, status.Errorf(codes.Internal, "failed to create Snapshot job: %v", err)
	}
	klog.InfoS("Snapshot job created", "job", jobName, "node", nodeName, "imgPath", imgPath, "snapshotPath", snapshotPath)

	// Wait for the job to complete
	if err := util.WaitForJobCompletion(ctx, cs.goClient, cs.namespace, jobName); err != nil {
		klog.ErrorS(err, "Snapshot job failed", "job", jobName)
		return nil, status.Errorf(codes.Internal, "Snapshot job failed: %v", err)
	}

	snapshot := hposv1.HPOSSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name: snapshotName,
			Finalizers: []string{
				"hposnapshot.k8s.io.need-deletion"},
		},
		Spec: hposv1.HPOSSnapshotSpec{

			SnapshotID:  snapshotName,
			SourceVolID: req.SourceVolumeId,
		},
	}
	err = cs.goClient.Create(ctx, &snapshot)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			klog.InfoS("Snapshot was created by another request, returning existing", "snapshotName", snapshotName)
			existing := &hposv1.HPOSSnapshot{}
			getErr := cs.goClient.Get(ctx, client.ObjectKey{Name: snapshotName}, existing)
			if getErr == nil {
				return &csi.CreateSnapshotResponse{
					Snapshot: &csi.Snapshot{
						SnapshotId:     existing.Spec.SnapshotID,
						SourceVolumeId: existing.Spec.SourceVolID,
						CreationTime:   timestamppb.Now(),
					},
				}, nil
			}
			klog.ErrorS(getErr, "Failed to fetch existing snapshot after AlreadyExists error", "snapshotName", snapshotName)
		}

		klog.ErrorS(err, "Internal Error: Error while creating HPOSSnapshot object")
		cleanupJobName := "cleanup-snapshot-" + req.Name[:16]
		cleanupJob := util.DeleteImageJob(cleanupJobName, cs.namespace, nodeName, snapshotPath)
		if cleanupErr := cs.goClient.Create(ctx, cleanupJob); cleanupErr != nil && !apierrors.IsAlreadyExists(cleanupErr) {
			klog.ErrorS(cleanupErr, "Failed to create cleanup job for snapshot", "job", cleanupJobName, "node", nodeName, "snapshotPath", snapshotPath)
		} else {
			klog.InfoS("Cleanup job for snapshot created", "job", cleanupJobName, "node", nodeName, "snapshotPath", snapshotPath)
			if waitErr := util.WaitForJobCompletion(ctx, cs.goClient, cs.namespace, cleanupJobName); waitErr != nil {
				klog.ErrorS(waitErr, "Cleanup job for snapshot failed", "job", cleanupJobName)
			} else {
				klog.InfoS("Cleanup job for snapshot completed successfully", "job", cleanupJobName)
			}
		}

		return nil, status.Errorf(codes.Internal, "Snapshot Creation failed: %v", err)
	}

	klog.InfoS("Snapshot job completed successfully", "job", jobName)

	return &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     snapshotName,
			SourceVolumeId: req.SourceVolumeId,
			CreationTime:   timestamppb.Now(),
			ReadyToUse:     true,
		},
	}, nil
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

func NewControllerService(goClient client.Client, namespace string) *ControllerService {
	return &ControllerService{goClient: goClient, namespace: namespace}
}
