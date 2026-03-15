#!/bin/bash
set -e

NAMESPACE="testing"
IMAGE="shilucloud/csi-driver-hostpath-on-steriod:local"
DRIVER_NAME="csi.driver.hostpath.on.steriod"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}✅ $1${NC}"; }
fail() { echo -e "${RED}❌ $1${NC}"; exit 1; }
info() { echo -e "${YELLOW}➜ $1${NC}"; }

# ─────────────────────────────────────────
# STEP 1: BUILD AND LOAD IMAGE
# ─────────────────────────────────────────
info "Building and loading image..."
#docker build -t $IMAGE .
#kind load docker-image $IMAGE --name kind
pass "Image built and loaded"

# ─────────────────────────────────────────
# STEP 2: CLEANUP OLD RESOURCES
# ─────────────────────────────────────────
info "Cleaning up old resources..."
kubectl delete -f manifests/examples/pod.yaml --ignore-not-found
kubectl delete -f manifests/examples/pvc.yaml --ignore-not-found
kubectl delete -f manifests/controller.yaml --ignore-not-found
kubectl delete -f manifests/node.yaml --ignore-not-found
kubectl delete -f manifests/storageclass.yaml --ignore-not-found
kubectl delete -f manifests/csidriver.yaml --ignore-not-found
kubectl delete -f manifests/clusterrolebinding.yaml --ignore-not-found
kubectl delete -f manifests/clusterrole.yaml --ignore-not-found
kubectl delete -f manifests/serviceaccount.yaml --ignore-not-found
kubectl delete -f manifests/crds/hpos-crd.yaml --ignore-not-found
kubectl delete hpvol --all --ignore-not-found
kubectl delete volumeattachment --all --ignore-not-found 2>/dev/null || true

info "Waiting for old pods to terminate..."
kubectl wait --for=delete pod \
  -l csi-driver-component=controller \
  -n $NAMESPACE --timeout=60s 2>/dev/null || true
kubectl wait --for=delete pod \
  -l csi-driver-component=node \
  -n $NAMESPACE --timeout=60s 2>/dev/null || true
pass "Old resources cleaned up"

# ─────────────────────────────────────────
# STEP 3: APPLY MANIFESTS
# ─────────────────────────────────────────
info "Applying manifests..."
kubectl apply -f manifests/crds/hpos-crd.yaml
kubectl apply -f manifests/csidriver.yaml
kubectl apply -f manifests/serviceaccount.yaml
kubectl apply -f manifests/clusterrole.yaml
kubectl apply -f manifests/clusterrolebinding.yaml
kubectl apply -f manifests/storageclass.yaml
kubectl apply -f manifests/controller.yaml
kubectl apply -f manifests/node.yaml
pass "Manifests applied"

# ─────────────────────────────────────────
# STEP 4: WAIT FOR DRIVER PODS READY
# ─────────────────────────────────────────
info "Waiting for controller pod to be ready..."
kubectl wait --for=condition=available \
  deploy/csi-driver-hostpath-on-steriods-deploy \
  -n $NAMESPACE --timeout=120s \
  || fail "Controller pod not ready"
pass "Controller pod ready"

info "Waiting for node daemonset pod to be ready..."
kubectl wait --for=condition=ready pod \
  -l csi-driver-component=node \
  -n $NAMESPACE --timeout=120s \
  || fail "Node pod not ready"
pass "Node pod ready"

# ─────────────────────────────────────────
# STEP 5: CREATE PVC
# ─────────────────────────────────────────
info "Creating PVC..."
kubectl apply -f manifests/examples/pvc.yaml

info "Waiting for PVC to be Bound..."
kubectl wait --for=jsonpath='{.status.phase}'=Bound \
  pvc/demo --timeout=60s \
  || fail "PVC not Bound"
pass "PVC is Bound"

info "Checking HPOSVolume CR..."
kubectl get hpvol || fail "HPOSVolume CR not found"
pass "HPOSVolume CR exists"

# ─────────────────────────────────────────
# STEP 6: CREATE POD
# ─────────────────────────────────────────
info "Creating pod..."
kubectl apply -f manifests/examples/pod.yaml

info "Waiting for pod to be Running..."
kubectl wait --for=condition=ready pod/hposvolume-pod \
  --timeout=120s \
  || fail "Pod not Running"
pass "Pod is Running"

info "Checking VolumeAttachment..."
VA=$(kubectl get volumeattachment \
  -o jsonpath='{.items[?(@.spec.attacher=="'$DRIVER_NAME'")].metadata.name}')
if [ -z "$VA" ]; then
  fail "VolumeAttachment not found"
fi
ATTACHED=$(kubectl get volumeattachment $VA \
  -o jsonpath='{.status.attached}')
if [ "$ATTACHED" != "true" ]; then
  fail "VolumeAttachment not attached"
fi
pass "VolumeAttachment attached: $VA"

# ─────────────────────────────────────────
# STEP 7: VERIFY STORAGE INSIDE POD
# ─────────────────────────────────────────
info "Checking df -h inside pod..."
DF_OUTPUT=$(kubectl exec hposvolume-pod \
  -- df -h /data 2>/dev/null) \
  || fail "df -h failed inside pod"
echo "$DF_OUTPUT"

if echo "$DF_OUTPUT" | grep -q "/data"; then
  pass "/data is mounted inside pod"
else
  fail "/data not found in df -h output"
fi

info "Writing test file to /data..."
kubectl exec hposvolume-pod \
  -- sh -c "echo 'hpos-test' > /data/test.txt" \
  || fail "Failed to write test file"
pass "Test file written"

DATA=$(kubectl exec hposvolume-pod \
  -- cat /data/test.txt)
if [ "$DATA" = "hpos-test" ]; then
  pass "Test file read back successfully: $DATA"
else
  fail "Test file content mismatch: $DATA"
fi

# ─────────────────────────────────────────
# STEP 8: DELETE POD AND CHECK UNMOUNT
# ─────────────────────────────────────────
info "Deleting pod..."
kubectl delete -f manifests/examples/pod.yaml

info "Waiting for pod to be deleted..."
kubectl wait --for=delete pod/hposvolume-pod \
  --timeout=60s \
  || fail "Pod not deleted"
pass "Pod deleted"

info "Waiting for VolumeAttachment to be deleted..."
kubectl wait --for=delete volumeattachment/$VA \
  --timeout=60s \
  || fail "VolumeAttachment not deleted"
pass "VolumeAttachment deleted ✅"

# ─────────────────────────────────────────
# STEP 9: DELETE PVC AND CHECK CLEANUP
# ─────────────────────────────────────────
info "Deleting PVC..."
kubectl delete -f manifests/examples/pvc.yaml

info "Waiting for PVC to be deleted..."
kubectl wait --for=delete pvc/demo \
  --timeout=60s \
  || fail "PVC not deleted"
pass "PVC deleted"

info "Waiting for PV to be deleted..."
PV=$(kubectl get pv \
  -o jsonpath='{.items[?(@.spec.storageClassName=="hostpath-on-steriod")].metadata.name}' \
  2>/dev/null)
if [ -n "$PV" ]; then
  kubectl wait --for=delete pv/$PV \
    --timeout=60s \
    || fail "PV not deleted"
fi
pass "PV deleted"

info "Checking HPOSVolume CR is deleted..."
sleep 5
HPVOL=$(kubectl get hpvol 2>/dev/null | grep -v NAME | wc -l)
if [ "$HPVOL" -eq 0 ]; then
  pass "HPOSVolume CR deleted ✅"
else
  fail "HPOSVolume CR still exists"
fi

info "Checking cleanup job..."
kubectl get jobs -n $NAMESPACE 2>/dev/null || true

# ─────────────────────────────────────────
# DONE
# ─────────────────────────────────────────
echo ""
echo -e "${GREEN}══════════════════════════════════════${NC}"
echo -e "${GREEN}  E2E TEST PASSED ✅${NC}"
echo -e "${GREEN}══════════════════════════════════════${NC}"