package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type HPOSVolumeSpec struct {
	VolID    string `json:"volID"`
	NodeName string `json:"nodeName"`
	ByteSize string `json:"byteSize"`
	FsType   string `json:"fsType"`
}

type HPOSVolumeStatus struct {
	Phase        string `json:"phase"`
	AttachedNode string `json:"attachedNode"`
}

type HPOSVolume struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              HPOSVolumeSpec   `json:"spec"`
	Status            HPOSVolumeStatus `json:"status,omitempty"`
}

type HPOSVolumeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HPOSVolume `json:"items"`
}

/// snapshot types

type HPOSSnapshotSpec struct {
	SnapshotID  string `json:"snapshotID"`
	SourceVolID string `json:"sourceVolID"`
}

type HPOSSnapshotStatus struct {
	Phase string `json:"phase"`
}

type HPOSSnapshot struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              HPOSSnapshotSpec   `json:"spec"`
	Status            HPOSSnapshotStatus `json:"status,omitempty"`
}

type HPOSSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HPOSSnapshot `json:"items"`
}
