package model

import (
	"crypto/sha256"
	"os"

	"github.com/allenliu88/kube-sidecar-injector/pkg/logger"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var IgnoredNamespaces = []string{
	metav1.NamespaceSystem,
	metav1.NamespacePublic,
}

const (
	AdmissionWebhookAnnotationInjectKey = "sidecar-injector-webhook.morven.me/inject"
	AdmissionWebhookAnnotationStatusKey = "sidecar-injector-webhook.morven.me/status"

	MODE_LABELS      = "labels"
	MODE_ANNOTATIONS = "annotations"
)

type ContainerConfig struct {
	Name            string `yaml:"name"`
	Image           string `yaml:"image"`
	ImagePullPolicy string `yaml:"imagePullPolicy"`
	VolumeMounts    []struct {
		Name      string `yaml:"name"`
		MountPath string `yaml:"mountPath"`
	} `yaml:"volumeMounts"`
}

type VolumeConfig struct {
	Name      string `yaml:"name"`
	ConfigMap struct {
		Name string `yaml:"name"`
	} `yaml:"configMap"`
}

type Config struct {
	Containers  []ContainerConfig `yaml:"containers"`
	Volumes     []VolumeConfig    `yaml:"volumes"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

type PatchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func LoadConfig(configFile string) (*Config, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	logger.InfoLogger.Printf("New configuration: sha256sum %x", sha256.Sum256(data))
	logger.InfoLogger.Printf("New configuration: %s", string(data))

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	logger.InfoLogger.Printf("New configuration object: %v", cfg)

	return &cfg, nil
}

func ToContainer(config ContainerConfig) corev1.Container {
	container := corev1.Container{
		Name:            config.Name,
		Image:           config.Image,
		ImagePullPolicy: corev1.PullPolicy(config.ImagePullPolicy),
		VolumeMounts:    []corev1.VolumeMount{},
	}

	for _, vm := range config.VolumeMounts {
		volumeMount := corev1.VolumeMount{
			Name:      vm.Name,
			MountPath: vm.MountPath,
		}
		container.VolumeMounts = append(container.VolumeMounts, volumeMount)
	}

	return container
}

func ToVolume(config VolumeConfig) corev1.Volume {
	volume := corev1.Volume{
		Name: config.Name,
	}

	if config.ConfigMap.Name != "" {
		volume.VolumeSource = corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: config.ConfigMap.Name,
				},
			},
		}
	}

	return volume
}
