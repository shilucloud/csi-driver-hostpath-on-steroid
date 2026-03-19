# csi-driver-hostpath-on-steroids

A Kubernetes CSI driver that implements the full CSI flow locally 
no cloud account needed. Uses loop device backed storage with
`attachRequired: true`, giving you the complete VolumeAttachment
lifecycle, topology support, and NodeStageVolume/NodePublishVolume
flow on a local kind or minikube cluster.

Built as a companion to the
[CSI Driver Internals blog series](https://medium.com/@shilu4577).

---

## Why this driver?

Most hostpath CSI drivers skip the interesting parts —
no attach step, no VolumeAttachment, no real block device.
This driver implements the full flow:

- `attachRequired: true` → full VolumeAttachment lifecycle 
- Loop device backed storage → real block device behavior 
- Topology support → WaitForFirstConsumer works correctly 
- HPOSVolume CRD as state store → survives pod restarts 
- Cleanup Job for DeleteVolume → cross-node file deletion 

---

## Blog Series

This driver is built step by step across a 5-part series:

| Part | Title |
|------|-------|
| [Part 1](https://medium.com/@shilu4577/managing-storage-in-kubernetes-a-deep-dive-into-csi-drivers-dd4592cc4bb2) | Managing Storage in Kubernetes: A Deep Dive into CSI Drivers |
| [Part 2](https://medium.com/@shilu4577/what-actually-happens-when-you-create-a-pvc-ca87267fd4f7) | What Actually Happens When You Create a PVC? |
| [Part 3](https://medium.com/@shilu4577/how-does-volume-reaches-your-container-2bac04d94d3e) | How Does a Volume Reach Your Container? |
| [Part 4](https://medium.com/aws-in-plain-english/building-a-csi-driver-from-scratch-in-go-controller-side-51ce2cce7344)| Building a CSI Driver from Scratch in Go (Controller Side) |
| Part 5 | Building a CSI Driver from Scratch in Go (Node Side) |

---

## Prerequisites

- Go 1.25+
- Docker
- kind or minikube (single node)
- kubectl
- Helm 3
- [Task](https://taskfile.dev) (optional but recommended)

---

## Installation

### Using Helm
```bash
helm install hpos-csi-driver \
  https://github.com/shilucloud/csi-driver-hostpath-on-steriod/releases/download/v0.1.0/csi-driver-hostpath-on-steriod-0.1.0.tgz \
  --namespace testing \
  --create-namespace
```

### Create a PVC
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: hostpath-on-steriod-demo
spec:
  storageClassName: hostpath-on-steriod
  resources:
    requests:
      storage: 1G
  accessModes:
    - ReadWriteOnce
  volumeMode: Filesystem
```

### Create a Pod
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: hposvolume-pod
spec:
  containers:
    - name: con1
      image: nginx
      volumeMounts:
        - name: data
          mountPath: /data
  volumes:
    - name: data
      persistentVolumeClaim:
        claimName: hostpath-on-steriod-demo
```

---

## Local Development

### Prerequisites
```bash
# start a kind cluster
kind create cluster
```

### Build and test
```bash
# build image and load into kind
task docker-build

### Run e2e tests
```bash
task e2e
```

### Useful commands
```bash
task logs-controller   # tail controller logs
task logs-node         # tail node logs
```

---

## Project Structure
```
csi-driver-hostpath-on-steriod/
  cmd/
    main.go              ← entry point
  pkg/
    driver/
      controller.go      ← Controller Service RPCs
      node.go            ← Node Service RPCs
      identity.go        ← Identity Service RPCs
      driver.go          ← Driver struct, gRPC server
      constant.go        ← Mode constants
    apis/v1/
      types.go           ← HPOSVolume CRD types
    clientgo/
      client.go          ← Kubernetes client
    util/
      util.go            ← filesystem utilities
  charts/                ← Helm chart
  manifests/             ← raw Kubernetes manifests
  scripts/               ← e2e and cleanup scripts
  Taskfile.yml
```

---

## How it works
```
CreateVolume
  → creates HPOSVolume CR in Kubernetes
  → pins volume to a specific node via topology

ControllerPublishVolume
  → updates HPOSVolume status to "attached"
  → returns imgPath in PublishContext

NodeStageVolume
  → creates .img file at /var/lib/hpos/<vol-id>.img
  → attaches as loop device (losetup)
  → formats with xfs/ext4 (mkfs)
  → mounts to global staging path

NodePublishVolume
  → bind mounts staging path into pod directory
  → container sees /data

DeleteVolume
  → creates cleanup Job on target node
  → Job runs rm -f on .img file
  → deletes HPOSVolume CR
```

---

## Limitations
```
- Single node clusters only (kind/minikube)
- volumeMode: Filesystem only
- ReadWriteOnce only
- Not production ready
```

---

## Requirements

- Kubernetes 1.20+
- Single node cluster (kind or minikube)

---

## License

MIT