package util

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	hposv1 "github.com/shilucloud/csi-driver-hostpath-on-steriod/pkg/apis/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ParseEndpoint(endpoint string) (string, string, error) {

	u, err := url.Parse(endpoint)
	if err != nil {
		return "", "", fmt.Errorf("could not parse endpoint: %w", err)
	}

	addr := filepath.Join(u.Host, filepath.FromSlash(u.Path))

	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "tcp":
	case "unix":
		addr = filepath.Join("/", addr)
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			return "", "", fmt.Errorf("could not remove unix domain socket %q: %w", addr, err)
		}
	default:
		return "", "", fmt.Errorf("unsupported protocol: %s", scheme)
	}

	return scheme, addr, nil
}

func GetHostName() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	return hostname
}

func GetNumberOfVolumesPerNode() int64 {
	return 25
}

func CreateImageFile(path string, byteSize int64) error {
	if fileExists(path) {
		return nil
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("error creating image file %s: %w", path, err)
	}
	f.Close()

	if err := os.Truncate(path, byteSize); err != nil {
		return fmt.Errorf("error sizing image file %s: %w", path, err)
	}
	return nil
}

func AttachLoopDevice(imgPath string) (string, error) {
	// check if already attached
	out, err := exec.Command("losetup", "-j", imgPath).Output()
	if err == nil && len(out) > 0 {
		loopDev := strings.SplitN(string(out), ":", 2)[0]
		return strings.TrimSpace(loopDev), nil
	}

	// get next free loop device number
	out, err = exec.Command("losetup", "-f").Output()
	if err != nil {
		return "", fmt.Errorf("losetup -f failed: %w", err)
	}
	loopDev := strings.TrimSpace(string(out))

	// create the device node if it doesn't exist
	exec.Command("mknod", loopDev, "b", "7", loopDevMinor(loopDev)).Run()

	// attach
	cmd := exec.Command("losetup", loopDev, imgPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("losetup %s %s failed: %w — %s", loopDev, imgPath, err, stderr.String())
	}

	return loopDev, nil
}

func DetachLoopDevice(stagingPath string) error {
	// find which loop device is mounted at staging path
	out, err := exec.Command("findmnt", "-n", "-o", "SOURCE", stagingPath).Output()
	if err != nil {
		return fmt.Errorf("findmnt %s: %w", stagingPath, err)
	}

	loopDev := strings.TrimSpace(string(out))
	if loopDev == "" {
		return fmt.Errorf("no device found at %s", stagingPath)
	}
	if !strings.HasPrefix(loopDev, "/dev/loop") {
		return nil // not a loop device, skip
	}

	cmd := exec.Command("losetup", "-d", loopDev)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("losetup -d %s failed: %w — %s", loopDev, err, stderr.String())
	}

	return nil
}

func loopDevMinor(loopDev string) string {
	// extract number from /dev/loopN
	n := strings.TrimPrefix(loopDev, "/dev/loop")
	return n
}

func MakeFs(devicePath, fsType string) error {
	// check if already has a filesystem — idempotency
	out, _ := exec.Command("blkid", "-o", "value", "-s", "TYPE", devicePath).Output()
	if strings.TrimSpace(string(out)) != "" {
		return nil
	}

	cmd := exec.Command("mkfs."+fsType, devicePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error making filesystem: %w: %s", err, string(out))
	}
	return nil
}

func Mount(source, target, fsType string) error {
	cmd := exec.Command("mount", "-t", fsType, source, target)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error mounting %s to %s: %w: %s", source, target, err, string(out))
	}
	return nil
}

func BindMount(source, target string) error {
	if err := os.MkdirAll(target, 0750); err != nil {
		return fmt.Errorf("failed to create target dir %s: %w", target, err)
	}

	cmd := exec.Command("mount", "--bind", source, target)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error mounting %s to %s: %w: %s", source, target, err, string(out))
	}
	return nil
}

// used in NodeUnpublishVolume — unmount AND remove dir
func Unmount(target string) error {
	cmd := exec.Command("umount", target)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error unmounting %s: %w: %s", target, err, string(out))
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("failed to remove target dir %s: %w", target, err)
	}
	return nil
}

// used in NodeUnstageVolume — unmount only, don't remove dir
func UnmountOnly(target string) error {
	cmd := exec.Command("umount", target)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("error unmounting %s: %w: %s", target, err, string(out))
	}
	return nil
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	fmt.Printf("Error checking file: %v\n", err)
	return false
}

func StrToInt(value string) (int64, error) {
	return strconv.ParseInt(value, 10, 64)
}

func Int32Ptr(i int32) *int32 { return &i }

func WaitForJobCompletion(ctx context.Context, goClient client.Client, namespace, jobName string) error {
	// Poll the job status until it completes or fails
	klog.InfoS("Waiting for job to complete", "job", jobName, "namespace", namespace)
	for {
		job := &batchv1.Job{}
		if err := goClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: jobName}, job); err != nil {
			return fmt.Errorf("failed to get job %s: %w", jobName, err)
		}

		for _, c := range job.Status.Conditions {
			if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
				return nil // Job completed successfully
			}
			if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
				return fmt.Errorf("job %s failed: %s", jobName, c.Message)
			}
		}

		time.Sleep(2 * time.Second) // Wait before polling again
	}
}

func IsAttachedLoopDevice(stagingPath string) (bool, error) {
	out, err := exec.Command("findmnt", "-n", "-o", "SOURCE", stagingPath).Output()
	if err != nil {
		return false, fmt.Errorf("findmnt %s: %w", stagingPath, err)
	}

	loopDev := strings.TrimSpace(string(out))
	if loopDev == "" {
		return false, nil // no device found at staging path
	}
	return strings.HasPrefix(loopDev, "/dev/loop"), nil
}

func IsMounted(targetPath string) (bool, error) {
	cmd := exec.Command("findmnt", "-n", "-o", "TARGET", targetPath)
	out, err := cmd.Output()
	if err != nil {
		// exit status 1 means "not mounted" for findmnt — not a real error
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("findmnt %s: %w", targetPath, err)
	}
	mountedPath := strings.TrimSpace(string(out))
	return mountedPath == targetPath, nil
}

func HasFinalizer(ctx context.Context, k8sClient client.Client, resourceName string) (bool, error) {

	vol := hposv1.HPOSVolume{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: resourceName}, &vol)

	if err != nil {
		return false, err
	}

	if len(vol.Finalizers) > 0 {
		return true, nil
	}

	return false, nil

}

func DeleteImageJob(jobName, namespace, nodeName, imgPath string) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: Int32Ptr(60),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					NodeSelector: map[string]string{
						"kubernetes.io/hostname": nodeName,
					},
					Containers: []corev1.Container{{
						Name:    "cleanup",
						Image:   "busybox",
						Command: []string{"rm", "-f", imgPath},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "hpos-data",
							MountPath: "/var/lib/hpos",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "hpos-data",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/var/lib/hpos",
							},
						},
					}},
				},
			},
		},
	}

	return job
}

func CreateSnapshotJob(jobName, namespace, nodeName, imgPath, snapshotPath string) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: Int32Ptr(60),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					NodeSelector: map[string]string{
						"kubernetes.io/hostname": nodeName,
					},
					Containers: []corev1.Container{{
						Name:    "cleanup",
						Image:   "busybox",
						Command: []string{"cp", imgPath, snapshotPath},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "hpos-data",
							MountPath: "/var/lib/hpos",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "hpos-data",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/var/lib/hpos",
							},
						},
					}},
				},
			},
		},
	}

	return job
}

func DeleteSnapshotJob(jobName, namespace, nodeName, snapshotPath string) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: Int32Ptr(60),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					NodeSelector: map[string]string{
						"kubernetes.io/hostname": nodeName,
					},
					Containers: []corev1.Container{{
						Name:    "cleanup",
						Image:   "busybox",
						Command: []string{"rm", "-f", snapshotPath},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "hpos-data",
							MountPath: "/var/lib/hpos",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "hpos-data",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{
								Path: "/var/lib/hpos",
							},
						},
					}},
				},
			},
		},
	}

	return job
}
