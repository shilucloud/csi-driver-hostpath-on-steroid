// pkg/apis/v1/zz_generated_deepcopy.go
package v1

import "k8s.io/apimachinery/pkg/runtime"

func (in *HPOSVolume) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(HPOSVolume)
	*out = *in
	return out
}

func (in *HPOSVolumeList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(HPOSVolumeList)
	*out = *in
	if in.Items != nil {
		out.Items = make([]HPOSVolume, len(in.Items))
		copy(out.Items, in.Items)
	}
	return out
}

// snapshot types

func (in *HPOSSnapshot) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(HPOSSnapshot)
	*out = *in
	return out
}

func (in *HPOSSnapshotList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(HPOSSnapshotList)
	*out = *in
	if in.Items != nil {
		out.Items = make([]HPOSSnapshot, len(in.Items))
		copy(out.Items, in.Items)
	}
	return out
}
