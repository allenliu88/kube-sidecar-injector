package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/allenliu88/kube-sidecar-injector/pkg/logger"
	"github.com/allenliu88/kube-sidecar-injector/pkg/model"
	"github.com/allenliu88/kube-sidecar-injector/pkg/util"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

type WebhookServer struct {
	SidecarConfig *model.Config
	Server        *http.Server
}

// Check whether the target resoured need to be mutated
func mutationRequired(ignoredList []string, metadata *metav1.ObjectMeta) bool {
	// skip special kubernete system namespaces
	for _, namespace := range ignoredList {
		if metadata.Namespace == namespace {
			logger.InfoLogger.Printf("Skip mutation for %v for it's in special namespace:%v", metadata.Name, metadata.Namespace)
			return false
		}
	}

	annotations := metadata.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	status := annotations[model.AdmissionWebhookAnnotationStatusKey]

	// determine whether to perform mutation based on annotation for the target resource
	var required bool
	if strings.ToLower(status) == "injected" {
		required = false
	} else {
		switch strings.ToLower(annotations[model.AdmissionWebhookAnnotationInjectKey]) {
		default:
			required = true
		case "n", "not", "false", "off":
			required = false
		}
	}

	logger.InfoLogger.Printf("Mutation policy for %v/%v: status: %q required:%v", metadata.Namespace, metadata.Name, status, required)
	return required
}

func addContainer(target []corev1.Container, added []model.ContainerConfig, basePath string) (patch []model.PatchOperation) {
	first := len(target) == 0
	var value interface{}
	for _, add := range added {
		container := model.ToContainer(add)
		value = container
		path := basePath

		if first {
			first = false
			value = []corev1.Container{container}
		} else {
			path = path + "/-"
		}

		patch = append(patch, model.PatchOperation{
			Op:    "add",
			Path:  path,
			Value: value,
		})
	}
	return patch
}

func addVolume(target []corev1.Volume, added []model.VolumeConfig, basePath string) (patch []model.PatchOperation) {
	first := len(target) == 0
	var value interface{}
	for _, add := range added {
		volume := model.ToVolume(add)
		value = volume
		path := basePath

		if first {
			first = false
			value = []corev1.Volume{volume}
		} else {
			path = path + "/-"
		}
		patch = append(patch, model.PatchOperation{
			Op:    "add",
			Path:  path,
			Value: value,
		})
	}
	return patch
}

func updateLabelsOrAnnotations(mode string, target, added map[string]string) (patch []model.PatchOperation) {
	first := len(target) == 0
	basePath := fmt.Sprintf("/metadata/%s", mode)

	for key, value := range added {
		if first {
			first = false
			patch = append(patch, model.PatchOperation{
				Op:   "add",
				Path: basePath,
				Value: map[string]string{
					key: value,
				},
			})
		} else if _, ok := target[key]; ok {
			patch = append(patch, model.PatchOperation{
				Op:    "replace",
				Path:  basePath + "/" + util.UniformKey(key),
				Value: value,
			})
		} else {
			patch = append(patch, model.PatchOperation{
				Op:    "add",
				Path:  basePath + "/" + util.UniformKey(key),
				Value: value,
			})
		}
	}
	return patch
}

// create mutation patch for resoures
func createPatch(pod *corev1.Pod, sidecarConfig *model.Config, annotations map[string]string) ([]byte, error) {
	var patch []model.PatchOperation

	// note: each function must be called one and only one times(it depends on its implemetation which make assumptions with the target object)
	// multiple added or updated objects must be merged first
	patch = append(patch, addContainer(pod.Spec.Containers, sidecarConfig.Containers, "/spec/containers")...)
	patch = append(patch, addVolume(pod.Spec.Volumes, sidecarConfig.Volumes, "/spec/volumes")...)
	patch = append(patch, updateLabelsOrAnnotations(model.MODE_ANNOTATIONS, pod.Annotations, util.MergeMaps(annotations, sidecarConfig.Annotations))...)
	patch = append(patch, updateLabelsOrAnnotations(model.MODE_LABELS, pod.Labels, sidecarConfig.Labels)...)

	return json.Marshal(patch)
}

// main mutation process
func (whsvr *WebhookServer) mutate(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	req := ar.Request
	var pod corev1.Pod
	if err := json.Unmarshal(req.Object.Raw, &pod); err != nil {
		logger.WarningLogger.Printf("Could not unmarshal raw object: %v", err)
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	logger.InfoLogger.Printf("AdmissionReview for Kind=%v, Namespace=%v Name=%v (%v) UID=%v patchOperation=%v UserInfo=%v",
		req.Kind, req.Namespace, req.Name, pod.Name, req.UID, req.Operation, req.UserInfo)

	// determine whether to perform mutation
	if !mutationRequired(model.IgnoredNamespaces, &pod.ObjectMeta) {
		logger.InfoLogger.Printf("Skipping mutation for %s/%s due to policy check", pod.Namespace, pod.Name)
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	annotations := map[string]string{model.AdmissionWebhookAnnotationStatusKey: "injected"}
	patchBytes, err := createPatch(&pod, whsvr.SidecarConfig, annotations)
	if err != nil {
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	logger.InfoLogger.Printf("AdmissionResponse: patch=%v\n", string(patchBytes))
	return &admissionv1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *admissionv1.PatchType {
			pt := admissionv1.PatchTypeJSONPatch
			return &pt
		}(),
	}
}

// Serve method for webhook server
func (whsvr *WebhookServer) serve(w http.ResponseWriter, r *http.Request) {
	logger.InfoLogger.Println("Starting to serve request ...")

	var body []byte
	if r.Body != nil {
		if data, err := io.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		logger.WarningLogger.Println("empty body")
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		logger.WarningLogger.Printf("Content-Type=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, expect `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	var admissionResponse *admissionv1.AdmissionResponse
	ar := admissionv1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		logger.WarningLogger.Printf("Can't decode body: %v", err)
		admissionResponse = &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
		admissionResponse = whsvr.mutate(&ar)
	}

	admissionReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
	}
	if admissionResponse != nil {
		admissionReview.Response = admissionResponse
		if ar.Request != nil {
			admissionReview.Response.UID = ar.Request.UID
		}
	}

	resp, err := json.Marshal(admissionReview)
	if err != nil {
		logger.WarningLogger.Printf("Can't encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	logger.InfoLogger.Printf("Ready to write reponse ...")
	if _, err := w.Write(resp); err != nil {
		logger.WarningLogger.Printf("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}
