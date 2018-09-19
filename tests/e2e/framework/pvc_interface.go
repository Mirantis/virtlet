/*
Copyright 2017 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package framework

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PVCSpec describes a PVC+PV pair to create.
type PVCSpec struct {
	// The name of PVC. The PV name is derived by adding the "-pv"
	// suffix.
	Name string
	// The size of PV. Must be parseable as k8s resource quantity
	// (e.g. 10M).
	Size string
	// If non-empty, specifies the node name for local PV.
	NodeName string
	// If true, specifies the block volume mode, otherwise
	// filesystem volume mode is used.
	Block bool
	// In block volume mode, the path to the block device inside the VM,
	// empty value means not referencing the volume in volumeDevices.
	// In filesystem volume mode, the path inside the VM for mounting the volume,
	// empty value means not mounting the volume.
	ContainerPath string
	// For local PVs, specifies the path to the directory when
	// using filesystem mode, and the path to the block device in
	// the block mode.
	LocalPath string
	// Ceph RBD image name.
	CephRBDImage string
	// Ceph monitor IP.
	CephMonitorIP string
	// Ceph pool name for the RBD.
	CephRBDPool string
	// The name of Kubernetes secret to use for Ceph.
	CephSecretName string
	// FlexVolume options for Virtlet flexvolume driver.
	FlexVolumeOptions map[string]string
}

func (spec PVCSpec) pvSource(namespace string) v1.PersistentVolumeSource {
	switch {
	case spec.LocalPath != "":
		if spec.CephRBDImage != "" || len(spec.FlexVolumeOptions) > 0 {
			panic("Can only use one of LocalPath, CephRBDImage and FlexVolumeOptions at the same time")
		}
		return v1.PersistentVolumeSource{
			Local: &v1.LocalVolumeSource{
				Path: spec.LocalPath,
			},
		}
	case spec.CephRBDImage != "" && len(spec.FlexVolumeOptions) > 0:
		panic("Can only use one of LocalPath, CephRBDImage and FlexVolumeOptions at the same time")
	case spec.CephRBDImage != "":
		return v1.PersistentVolumeSource{
			RBD: &v1.RBDPersistentVolumeSource{
				CephMonitors: []string{spec.CephMonitorIP},
				RBDImage:     spec.CephRBDImage,
				RBDPool:      spec.CephRBDPool,
				SecretRef: &v1.SecretReference{
					Name:      spec.CephSecretName,
					Namespace: namespace,
				},
			},
		}
	case len(spec.FlexVolumeOptions) > 0:
		return v1.PersistentVolumeSource{
			FlexVolume: &v1.FlexPersistentVolumeSource{
				Driver:  "virtlet/flexvolume_driver",
				Options: spec.FlexVolumeOptions,
			},
		}
	default:
		panic("bad PV/PVC spec")
	}
}

func (spec PVCSpec) nodeAffinity() *v1.VolumeNodeAffinity {
	if spec.NodeName == "" {
		return nil
	}
	return &v1.VolumeNodeAffinity{
		Required: &v1.NodeSelector{
			NodeSelectorTerms: []v1.NodeSelectorTerm{
				{
					MatchExpressions: []v1.NodeSelectorRequirement{
						{
							Key:      "kubernetes.io/hostname",
							Operator: "In",
							Values:   []string{spec.NodeName},
						},
					},
				},
			},
		},
	}
}

// PVCInterface is used to work with PersistentVolumes (PVs) and PersistentVolumeClaims (PVCs).
type PVCInterface struct {
	controller *Controller
	// Spec for the PV and PVC.
	Spec PVCSpec
	// Kubernetes PV object
	Volume *v1.PersistentVolume
	// Kubernetes PVC object
	Claim *v1.PersistentVolumeClaim
}

func newPersistentVolumeClaim(controller *Controller, spec PVCSpec) *PVCInterface {
	volMode := v1.PersistentVolumeFilesystem
	if spec.Block {
		volMode = v1.PersistentVolumeBlock
	}
	return &PVCInterface{
		controller: controller,
		Spec:       spec,
		Volume: &v1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: spec.Name + "-pv",
			},
			Spec: v1.PersistentVolumeSpec{
				Capacity: v1.ResourceList{
					v1.ResourceStorage: resource.MustParse(spec.Size),
				},
				VolumeMode:  &volMode,
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				ClaimRef: &v1.ObjectReference{
					Name:      spec.Name,
					Namespace: controller.Namespace(),
				},
				PersistentVolumeSource: spec.pvSource(controller.Namespace()),
				NodeAffinity:           spec.nodeAffinity(),
			},
		},
		Claim: &v1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: spec.Name,
			},
			Spec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				VolumeMode:  &volMode,
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceStorage: resource.MustParse(spec.Size),
					},
				},
			},
		},
	}
}

// AddToPod adds the volume to the pod referencing the PVC.
func (pvci *PVCInterface) AddToPod(pi *PodInterface, name string) {
	pi.Pod.Spec.Volumes = append(pi.Pod.Spec.Volumes, v1.Volume{
		Name: name,
		VolumeSource: v1.VolumeSource{
			PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
				ClaimName: pvci.Claim.Name,
			},
		},
	})
	if pvci.Spec.ContainerPath != "" {
		c := &pi.Pod.Spec.Containers[0]
		if pvci.Spec.Block {
			c.VolumeDevices = append(c.VolumeDevices, v1.VolumeDevice{
				Name:       name,
				DevicePath: pvci.Spec.ContainerPath,
			})
		} else {
			c.VolumeMounts = append(c.VolumeMounts, v1.VolumeMount{
				Name:      name,
				MountPath: pvci.Spec.ContainerPath,
			})
		}
	}
}

// Create creates the PVC and its corresponding PV.
func (pvci *PVCInterface) Create() error {
	updatedPV, err := pvci.controller.PersistentVolumesClient().Create(pvci.Volume)
	if err != nil {
		return err
	}
	pvci.Volume = updatedPV
	updatedPVC, err := pvci.controller.PersistentVolumeClaimsClient().Create(pvci.Claim)
	if err != nil {
		return err
	}
	pvci.Claim = updatedPVC
	return nil
}

// Delete deletes the PVC and its corresponding PV.
// It doesn't return an error if either PVC or PV doesn't exist.
func (pvci *PVCInterface) Delete() error {
	var errs []string
	if err := pvci.controller.PersistentVolumeClaimsClient().Delete(pvci.Claim.Name, nil); err != nil && !k8serrors.IsNotFound(err) {
		errs = append(errs, fmt.Sprintf("error deleting pvc %q: %v", pvci.Claim.Name, err))
	}
	if err := pvci.controller.PersistentVolumesClient().Delete(pvci.Volume.Name, nil); err != nil && !k8serrors.IsNotFound(err) {
		errs = append(errs, fmt.Sprintf("error deleting pv %q: %v", pvci.Volume.Name, err))
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, "\n"))
}

// WaitForDestruction waits for the PV and PVC to be deleted.
func (pvci *PVCInterface) WaitForDestruction(timing ...time.Duration) error {
	timeout := time.Minute * 5
	pollPeriond := time.Second
	consistencyPeriod := time.Second * 5
	if len(timing) > 0 {
		timeout = timing[0]
	}
	if len(timing) > 1 {
		pollPeriond = timing[1]
	}
	if len(timing) > 2 {
		consistencyPeriod = timing[2]
	}

	return waitForConsistentState(func() error {
		switch _, err := pvci.controller.PersistentVolumeClaimsClient().Get(pvci.Claim.Name, metav1.GetOptions{}); {
		case err == nil:
			return errors.New("PVC not deleted")
		case !k8serrors.IsNotFound(err):
			return err
		}
		switch _, err := pvci.controller.PersistentVolumesClient().Get(pvci.Volume.Name, metav1.GetOptions{}); {
		case err == nil:
			return errors.New("PV not deleted")
		case !k8serrors.IsNotFound(err):
			return err
		}
		return nil
	}, timeout, pollPeriond, consistencyPeriod)
}
