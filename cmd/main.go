package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/allenliu88/kube-sidecar-injector/pkg/logger"
	"github.com/allenliu88/kube-sidecar-injector/pkg/model"
)

var (
	port                                 int
	sidecarConfigFile                    string
	webhookNamespace, webhookServiceName string
)

func init() {
	// webhook server running namespace
	webhookNamespace = os.Getenv("POD_NAMESPACE")
}

func main() {
	// init command flags
	flag.IntVar(&port, "port", 8443, "Webhook server port.")
	flag.StringVar(&webhookServiceName, "service-name", "sidecar-injector", "Webhook service name.")
	flag.StringVar(&sidecarConfigFile, "sidecar-config-file", "/etc/webhook/config/sidecarconfig.yaml", "Sidecar injector configuration file.")
	// flag.StringVar(&certFile, "tlsCertFile", "/etc/webhook/certs/cert.pem", "x509 Certificate file.")
	// flag.StringVar(&keyFile, "tlsKeyFile", "/etc/webhook/certs/key.pem", "x509 private key file.")
	flag.Parse()

	dnsNames := []string{
		webhookServiceName,
		webhookServiceName + "." + webhookNamespace,
		webhookServiceName + "." + webhookNamespace + ".svc",
	}
	commonName := webhookServiceName + "." + webhookNamespace + ".svc"

	org := "morven.me"
	caPEM, certPEM, certKeyPEM, err := generateCert([]string{org}, dnsNames, commonName)
	if err != nil {
		logger.ErrorLogger.Fatalf("Failed to generate ca and certificate key pair: %v", err)
	}

	pair, err := tls.X509KeyPair(certPEM.Bytes(), certKeyPEM.Bytes())
	if err != nil {
		logger.ErrorLogger.Fatalf("Failed to load certificate key pair: %v", err)
	}

	sidecarConfig, err := model.LoadConfig(sidecarConfigFile)
	if err != nil {
		logger.ErrorLogger.Fatalf("Failed to load configuration: %v", err)
	}

	// create or update the mutatingwebhookconfiguration
	err = createOrUpdateMutatingWebhookConfiguration(caPEM, webhookServiceName, webhookNamespace)
	if err != nil {
		logger.ErrorLogger.Fatalf("Failed to create or update the mutating webhook configuration: %v", err)
	}

	whsvr := &WebhookServer{
		SidecarConfig: sidecarConfig,
		Server: &http.Server{
			Addr:      fmt.Sprintf(":%v", port),
			TLSConfig: &tls.Config{Certificates: []tls.Certificate{pair}},
		},
	}

	// define http server and server handler
	mux := http.NewServeMux()
	mux.HandleFunc(webhookInjectPath, whsvr.serve)
	whsvr.Server.Handler = mux

	// start webhook server in new rountine
	go func() {
		if err := whsvr.Server.ListenAndServeTLS("", ""); err != nil {
			logger.ErrorLogger.Fatalf("Failed to listen and serve webhook server: %v", err)
		}
	}()

	// listening OS shutdown singal
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan

	logger.InfoLogger.Printf("Got OS shutdown signal, shutting down webhook server gracefully...")
	whsvr.Server.Shutdown(context.Background())
}
