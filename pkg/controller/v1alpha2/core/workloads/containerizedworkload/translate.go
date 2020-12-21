/*
Copyright 2020 The Crossplane Authors.

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

package containerizedworkload

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"k8s.io/apimachinery/pkg/runtime"
	"path"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/crossplane/oam-kubernetes-runtime2/apis/core/v1alpha2"
	"github.com/crossplane/oam-kubernetes-runtime2/pkg/oam"
	"github.com/crossplane/oam-kubernetes-runtime2/pkg/oam/util"
)

var (
	deploymentKind        = reflect.TypeOf(appsv1.Deployment{}).Name()
	deploymentAPIVersion  = appsv1.SchemeGroupVersion.String()
	serviceKind           = reflect.TypeOf(corev1.Service{}).Name()
	serviceAPIVersion     = corev1.SchemeGroupVersion.String()
	statefulKind          = reflect.TypeOf(appsv1.StatefulSet{}).Name()
	statefulSetAPIVersion = appsv1.SchemeGroupVersion.String()
	configMapKind         = reflect.TypeOf(corev1.ConfigMap{}).Name()
	configMapAPIVersion   = corev1.SchemeGroupVersion.String()
)

// Reconcile error strings.
const (
	labelKey = "containerizedworkload.oam.crossplane.io"

	errNotContainerizedWorkload = "object is not a containerized workload"
)

type ChildWorkload struct {
	Type string
	runtime.TypeMeta
	metav1.ObjectMeta
	StsSpec appsv1.StatefulSetSpec
	DepSpec appsv1.DeploymentSpec

}
// TranslateContainerWorkload translates a ContainerizedWorkload into a Deployment.
// nolint:gocyclo
func TranslateContainerWorkload(w oam.Workload) (oam.Object, error) {
	cw, ok := w.(*v1alpha2.ContainerizedWorkload)
	if !ok {
		return nil, errors.New(errNotContainerizedWorkload)
	}

	labels := map[string]string{
		util.LabelAppId:       cw.Labels[util.LabelAppId],
		util.LabelComponentId: cw.GetName(),
	}
	d := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       deploymentKind,
			APIVersion: deploymentAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cw.GetName(),
			Namespace: cw.GetNamespace(),
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
			},
		},
	}

	for _, container := range cw.Spec.Containers {
		if container.ImagePullSecret != nil {
			d.Spec.Template.Spec.ImagePullSecrets = append(d.Spec.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{
				Name: *container.ImagePullSecret,
			})
		}
		kubernetesContainer := corev1.Container{
			Name:    container.Name,
			Image:   container.Image,
			Command: container.Command,
			Args:    container.Arguments,
		}

		if container.Resources != nil {
			kubernetesContainer.Resources = corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    container.Resources.CPU.Required,
					corev1.ResourceMemory: container.Resources.Memory.Required,
				},
			}
			for _, v := range container.Resources.Volumes {
				mount := corev1.VolumeMount{
					Name:      v.Name,
					MountPath: v.MountPath,
				}
				if v.AccessMode != nil && *v.AccessMode == v1alpha2.VolumeAccessModeRO {
					mount.ReadOnly = true
				}
				kubernetesContainer.VolumeMounts = append(kubernetesContainer.VolumeMounts, mount)

			}
		}

		for _, p := range container.Ports {
			port := corev1.ContainerPort{
				Name:          p.Name,
				ContainerPort: p.Port,
			}
			if p.Protocol != nil {
				port.Protocol = corev1.Protocol(*p.Protocol)
			}else{
				port.Protocol = corev1.ProtocolTCP
			}
			kubernetesContainer.Ports = append(kubernetesContainer.Ports, port)
		}

		for _, e := range container.Environment {
			if e.Value != nil {
				kubernetesContainer.Env = append(kubernetesContainer.Env, corev1.EnvVar{
					Name:  e.Name,
					Value: *e.Value,
				})
				continue
			}
			if e.FromSecret != nil {
				kubernetesContainer.Env = append(kubernetesContainer.Env, corev1.EnvVar{
					Name: e.Name,
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							Key: e.FromSecret.Key,
							LocalObjectReference: corev1.LocalObjectReference{
								Name: e.FromSecret.Name,
							},
						},
					},
				})
			}
		}


		/*
			1.目录挂载
				对vm 名称去重
			2.子文件挂载
				对vm
		*/
		vmMap := map[string]struct{}{}
		for _, c := range container.ConfigFiles {
			v, vm := translateConfigFileToVolume(c, w.GetName(), container.Name)
			if _, ok := vmMap[vm.Name]; ok {
				continue
			}
			vmMap[vm.Name] = struct{}{}
			kubernetesContainer.VolumeMounts = append(kubernetesContainer.VolumeMounts, vm)
			d.Spec.Template.Spec.Volumes = append(d.Spec.Template.Spec.Volumes, v)
		}
		d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, kubernetesContainer)
	}
	return d, nil
}


func translateConfigFileToVolume(cf v1alpha2.ContainerConfigFile, wlName, containerName string) (v corev1.Volume, vm corev1.VolumeMount) {
	mountPath, fileName := path.Split(cf.Path)
	// translate into ConfigMap Volume
	if cf.Value != nil {
		name, _ := generateConfigMapName(mountPath, wlName, containerName)
		v = corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: name},
				},
			},
		}
		vm = corev1.VolumeMount{
			MountPath: mountPath,
			Name:      name,
		}

		if cf.SubPath {
			name, _ := generateConfigMapName(cf.Path, wlName, containerName)
			v.Name = name
			vm.Name = name
			vm.SubPath = fileName //既是挂载的文件名，又是configMap 的key
			vm.MountPath = cf.Path
		}
		return v, vm
	}

	// translate into Secret Volume
	secretName := cf.FromSecret.Name
	itemKey := cf.FromSecret.Key
	v = corev1.Volume{
		Name: secretName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
				Items: []corev1.KeyToPath{
					{
						Key: itemKey,
						// OAM v1alpha2 SecretKeySelector doen't provide Path field
						// just use itemKey as relative Path
						Path: itemKey,
					},
				},
			},
		},
	}
	vm = corev1.VolumeMount{
		MountPath: cf.Path,
		Name:      secretName,
	}
	return v, vm
}

func generateConfigMapName(mountPath, wlName, containerName string) (string, error) {
	h := fnv.New32a()
	_, err := h.Write([]byte(mountPath))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%d", wlName, containerName, h.Sum32()), nil
}

// TranslateConfigMaps translate non-secret ContainerConfigFile into ConfigMaps
func TranslateConfigMaps(ctx context.Context, w oam.Object) (map[string]*corev1.ConfigMap, error) {
	cw, ok := w.(*v1alpha2.ContainerizedWorkload)
	if !ok {
		return nil, errors.New(errNotContainerizedWorkload)
	}

	newConfigMaps := map[string]*corev1.ConfigMap{}
	for _, c := range cw.Spec.Containers {
		for _, cf := range c.ConfigFiles {
			if cf.Value == nil {
				continue
			}
			mountPath, key := path.Split(cf.Path)
			cmName, err := generateConfigMapName(mountPath, cw.GetName(), c.Name)
			if err != nil {
				return nil, err
			}
			if _, ok := newConfigMaps[cmName]; ok {
				newConfigMaps[cmName].Data[key] = *cf.Value
			} else {
				cm := &corev1.ConfigMap{
					TypeMeta: metav1.TypeMeta{
						Kind:       configMapKind,
						APIVersion: configMapAPIVersion,
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmName,
						Namespace: cw.GetNamespace(),
					},
					Data: map[string]string{
						key: *cf.Value,
					},
				}
				// pass through label and annotation from the workload to the configmap
				util.PassLabelAndAnnotation(w, cm)
				newConfigMaps[cmName] = cm
			}
		}
	}
	return newConfigMaps, nil
}

// ServiceInjector adds a Service object for the first Port on the first
// Container for the first Deployment observed in a workload translation.
func ServiceInjector(ctx context.Context, w oam.Workload, obj runtime.Object) (*corev1.Service, error) {
	if obj == nil {
		return nil, nil
	}
	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       serviceKind,
			APIVersion: serviceAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      w.GetName(),
			Namespace: w.GetNamespace(),
			Labels: map[string]string{
				util.LabelAppId: w.GetLabels()[util.LabelAppId],
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				util.LabelComponentId: w.GetName(),
			},
			Ports: []corev1.ServicePort{},
			Type:  corev1.ServiceTypeClusterIP,
		},
	}

	var containers corev1.Container
	d, ok := obj.(*appsv1.Deployment)
	if ok {
		if len(d.Spec.Template.Spec.Containers) == 0 || len(d.Spec.Template.Spec.Containers[0].Ports) == 0 {
			return nil, nil
		}
		containers = d.Spec.Template.Spec.Containers[0]
	}

	s, ok := obj.(*appsv1.StatefulSet)
	if ok {
		if len(s.Spec.Template.Spec.Containers) == 0 || len(s.Spec.Template.Spec.Containers[0].Ports) == 0 {
			return nil, nil
		}
		containers = s.Spec.Template.Spec.Containers[0]
	}

	for _, c := range containers.Ports {
		svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
			Name:       c.Name,
			Protocol:   c.Protocol,
			Port:       c.ContainerPort,
			TargetPort: intstr.FromInt(int(c.ContainerPort)),
		})
	}
	return svc, nil
}

func TransDepToSts(deploy *appsv1.Deployment)oam.Object{
	sts := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       statefulKind,
			APIVersion: statefulSetAPIVersion,
		},
		ObjectMeta: deploy.ObjectMeta,
		Spec: appsv1.StatefulSetSpec{
			ServiceName: deploy.Name,
			Selector: deploy.Spec.Selector,
			Template: deploy.Spec.Template,
		},
	}
	return sts
}
