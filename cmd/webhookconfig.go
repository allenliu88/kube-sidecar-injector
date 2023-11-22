package main

import (
	"bytes"
	"context"
	"os"
	"reflect"

	"github.com/allenliu88/kube-sidecar-injector/pkg/logger"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	webhookConfigName = "sidecar-injector-webhook"
	webhookInjectPath = "/inject"
)

func createOrUpdateMutatingWebhookConfiguration(caPEM *bytes.Buffer, webhookService, webhookNamespace string) error {
	logger.InfoLogger.Println("Initializing the kube client...")

	kubeconfig := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	mutatingWebhookConfigV1Client := clientset.AdmissionregistrationV1()

	logger.InfoLogger.Printf("Creating or updating the mutatingwebhookconfiguration: %s", webhookConfigName)
	fail := admissionregistrationv1.Fail
	sideEffect := admissionregistrationv1.SideEffectClassNone
	mutatingWebhookConfig := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookConfigName,
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{{
			Name:                    "sidecar-injector.morven.me",
			AdmissionReviewVersions: []string{"v1", "v1beta1"},
			SideEffects:             &sideEffect,
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				CABundle: caPEM.Bytes(), // self-generated CA for the webhook
				Service: &admissionregistrationv1.ServiceReference{
					Name:      webhookService,
					Namespace: webhookNamespace,
					Path:      &webhookInjectPath,
				},
			},
			Rules: []admissionregistrationv1.RuleWithOperations{
				{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"pods"},
					},
				},
			},
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"sidecar-injection": "enabled",
				},
			},
			FailurePolicy: &fail,
		}},
	}

	foundWebhookConfig, err := mutatingWebhookConfigV1Client.MutatingWebhookConfigurations().Get(context.TODO(), webhookConfigName, metav1.GetOptions{})
	if err != nil && apierrors.IsNotFound(err) {
		if _, err := mutatingWebhookConfigV1Client.MutatingWebhookConfigurations().Create(context.TODO(), mutatingWebhookConfig, metav1.CreateOptions{}); err != nil {
			logger.WarningLogger.Printf("Failed to create the mutatingwebhookconfiguration: %s", webhookConfigName)
			return err
		}
		logger.InfoLogger.Printf("Created mutatingwebhookconfiguration: %s", webhookConfigName)
	} else if err != nil {
		logger.WarningLogger.Printf("Failed to check the mutatingwebhookconfiguration: %s", webhookConfigName)
		return err
	} else {
		// there is an existing mutatingWebhookConfiguration
		if len(foundWebhookConfig.Webhooks) != len(mutatingWebhookConfig.Webhooks) ||
			!(foundWebhookConfig.Webhooks[0].Name == mutatingWebhookConfig.Webhooks[0].Name &&
				reflect.DeepEqual(foundWebhookConfig.Webhooks[0].AdmissionReviewVersions, mutatingWebhookConfig.Webhooks[0].AdmissionReviewVersions) &&
				reflect.DeepEqual(foundWebhookConfig.Webhooks[0].SideEffects, mutatingWebhookConfig.Webhooks[0].SideEffects) &&
				reflect.DeepEqual(foundWebhookConfig.Webhooks[0].FailurePolicy, mutatingWebhookConfig.Webhooks[0].FailurePolicy) &&
				reflect.DeepEqual(foundWebhookConfig.Webhooks[0].Rules, mutatingWebhookConfig.Webhooks[0].Rules) &&
				reflect.DeepEqual(foundWebhookConfig.Webhooks[0].NamespaceSelector, mutatingWebhookConfig.Webhooks[0].NamespaceSelector) &&
				reflect.DeepEqual(foundWebhookConfig.Webhooks[0].ClientConfig.CABundle, mutatingWebhookConfig.Webhooks[0].ClientConfig.CABundle) &&
				reflect.DeepEqual(foundWebhookConfig.Webhooks[0].ClientConfig.Service, mutatingWebhookConfig.Webhooks[0].ClientConfig.Service)) {
			mutatingWebhookConfig.ObjectMeta.ResourceVersion = foundWebhookConfig.ObjectMeta.ResourceVersion
			if _, err := mutatingWebhookConfigV1Client.MutatingWebhookConfigurations().Update(context.TODO(), mutatingWebhookConfig, metav1.UpdateOptions{}); err != nil {
				logger.WarningLogger.Printf("Failed to update the mutatingwebhookconfiguration: %s", webhookConfigName)
				return err
			}
			logger.InfoLogger.Printf("Updated the mutatingwebhookconfiguration: %s", webhookConfigName)
		}
		logger.InfoLogger.Printf("The mutatingwebhookconfiguration: %s already exists and has no change", webhookConfigName)
	}

	return nil
}
