# kube-sidecar-injector

This repo is used for [a tutorial at Medium](https://medium.com/ibm-cloud/diving-into-kubernetes-mutatingadmissionwebhook-6ef3c5695f74) to create a Kubernetes [MutatingAdmissionWebhook](https://kubernetes.io/docs/admin/admission-controllers/#mutatingadmissionwebhook-beta-in-19) that injects a nginx sidecar container into pod prior to persistence of the object.

## Prerequisites

- [git](https://git-scm.com/downloads)
- [go](https://golang.org/dl/) version v1.17+
- [docker](https://docs.docker.com/install/) version 19.03+
- [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/) version v1.19+
- Access to a Kubernetes v1.19+ cluster with the `admissionregistration.k8s.io/v1` API enabled. Verify that by the following command:

```
kubectl api-versions | grep admissionregistration.k8s.io
```
The result should be:
```
admissionregistration.k8s.io/v1
admissionregistration.k8s.io/v1beta1
```

> Note: In addition, the `MutatingAdmissionWebhook` and `ValidatingAdmissionWebhook` admission controllers should be added and listed in the correct order in the admission-control flag of kube-apiserver.

## Build and Deploy

0. Provision a kubernetes cluster:

```bash
## Optional
minikube delete

## Start a cluster
minikube start
```

1. Build and push docker image:

```bash
make docker-build docker-push IMAGE=allen88/sidecar-injector:latest
```

2. Deploy the kube-sidecar-injector to kubernetes cluster:

> Please download and update the `bin/kustomize` to match the local machine architecture, eg. [kustomize_v5.2.1_darwin_arm64.tar.gz](https://github.com/kubernetes-sigs/kustomize/releases).

```bash
make deploy IMAGE=allen88/sidecar-injector:latest
```

3. Verify the kube-sidecar-injector is up and running:

```bash
# kubectl -n sidecar-injector get pod
NAME                                READY   STATUS    RESTARTS   AGE
sidecar-injector-7c8bc5f4c9-28c84   1/1     Running   0          30s

## 注意：提前确认启动日志，Created mutatingwebhookconfiguration: sidecar-injector-webhook
## 再继续后续验证操作！！！
# kubectl logs -f -n sidecar-injector deploy/sidecar-injector
INFO: 2023/11/22 10:52:51 model.go:61: New configuration: sha256sum ca4af226fae106a89db656d6598ed51262ecfbe6325645d09649a2c2b68e0b5c
INFO: 2023/11/22 10:52:51 model.go:62: New configuration: containers:
- name: sidecar-nginx
  image: nginx:1.12.2
  imagePullPolicy: IfNotPresent
  volumeMounts:
  - name: nginx-conf
    mountPath: /etc/nginx
volumes:
- name: nginx-conf
  configMap:
    name: nginx-configmap
labels:
  hskp.io/managed: true
annotations:
  hskp.io/injected: true
INFO: 2023/11/22 10:52:51 model.go:69: New configuration object: {[{sidecar-nginx nginx:1.12.2 IfNotPresent [{nginx-conf /etc/nginx}]}] [{nginx-conf {nginx-configmap}}] map[hskp.io/managed:true] map[hskp.io/injected:true]}
INFO: 2023/11/22 10:52:51 webhookconfig.go:23: Initializing the kube client...
W1122 10:52:51.340580       1 client_config.go:608] Neither --kubeconfig nor --master was specified.  Using the inClusterConfig.  This might not work.
INFO: 2023/11/22 10:52:51 webhookconfig.go:36: Creating or updating the mutatingwebhookconfiguration: sidecar-injector-webhook
INFO: 2023/11/22 10:52:51 webhookconfig.go:83: Created mutatingwebhookconfiguration: sidecar-injector-webhook
```

## How to use

1. Create a new namespace `test-ns` and label it with `sidecar-injector=enabled`:

```bash
# kubectl create ns test-ns
# kubectl label namespace test-ns sidecar-injection=enabled
# kubectl get namespace -L sidecar-injection
NAME                 STATUS   AGE   SIDECAR-INJECTION
default              Active   26m
test-ns              Active   13s   enabled
kube-public          Active   26m
kube-system          Active   26m
sidecar-injector     Active   17m
```

2. Deploy an app in Kubernetes cluster, take `alpine` app as an example

```bash
## 初始化ConfigMap
# kubectl -n test-ns apply -f deploy/nginx-configmap.yaml
# kubectl -n test-ns run alpine \
    --image=alpine \
    --restart=Never \
    --command -- sleep infinity

# kubectl -n test-ns delete po alpine
```

3. Verify sidecar container is injected:

```bash
# kubectl -n test-ns get pod
NAME                     READY     STATUS        RESTARTS   AGE
alpine                   2/2       Running       0          10s

# kubectl -n test-ns get pod alpine -o yaml

# kubectl -n test-ns get pod alpine -o jsonpath="{.spec.containers[*].name}"
alpine sidecar-nginx
```

## Troubleshooting

Sometimes you may find that pod is injected with sidecar container as expected, check the following items:

1. The `sidecar-injector` pod is in running state and no error logs.
2. The namespace in which application pod is deployed has the correct labels(`sidecar-injector=enabled`) as configured in `MutatingWebhookConfiguration`.
3. Check if the application pod has annotation `sidecar-injector-webhook.morven.me/inject: "yes"`.

## QA

### 1. can we use corev1.Container struct directly with unmashal?

No, you cannot directly unmarshal YAML into the  `corev1.Container`  struct because it is not a plain Go struct. The  `corev1.Container`  struct is part of the Kubernetes client library and includes additional functionality and methods specific to Kubernetes.   To convert YAML data into a  `corev1.Container`  object, you need to define a custom Go struct that matches the structure of the YAML and then manually map the fields to the  `corev1.Container`  object.   In the previous example, we defined a custom struct to hold the YAML data and then mapped the fields to the  `corev1.Container`  object. This approach allows you to extract the required fields from the YAML and create the  `corev1.Container`  object accordingly.

### 2. [Create a patch to add a kubernetes annotation](https://stackoverflow.com/questions/55573724/create-a-patch-to-add-a-kubernetes-annotation)

#### Question

I would like to write an [mutating webhook](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#mutatingadmissionwebhook) to add a default ingress class to all ingress object, that do not explicitly provide one.

According to the [examples](https://github.com/kubernetes/kubernetes/blob/master/test/images/webhook/addlabel.go#L29) I found I need to provide a proper json patch for the webhook to return.

I first tried my patches using kubectl:

```rust
$ kubectl patch ingress mying --type='json' -p='[{"op": "add", "path": "/metadata/annotations/key", "value":"value"}]'
The  "" is invalid
```

Looks like this is not working when there is not already an annotations element present.

```rust
$ kubectl patch ingress mying --type='json' -p='[{"op": "add", "path": "/metadata/annotations", "value":{"key":"value"}}]'
ingress.extensions/kafka-monitoring-topics-ui patched
```

Creating the complete annotations element works fine, however in my case I need a key of `kubernetes.io/ingress.class` which contains a slash.

```rust
kubectl patch ingress mying --type='json' -p='[{"op": "add", "path": "/metadata/annotations", "value":{"kubernetes.io/ingress.class":"value"}}]'
ingress.extensions/kafka-monitoring-topics-ui patched
```

This works fine when creating the annotation object. However, if there is already a some annotation present and I simply want to *add* one, it seems to be impossible to add one.

Simply using `[{"op": "add", "path": "/metadata/annotations", "value":{"kubernetes.io/ingress.class":"value"}}]` removes all existing annotation, while something like `'[{"op": "add", "path": "/metadata/annotations/kubernetes.io/ingress.class", "value": "value"}]` does not work because of the contained slash.

**Long story short: What is the correct way to simply add a ingress class using a proper patch?**

PS: Yes, I am aware of `kubectl annotate`, but unfortunately that does not help with my webhook.

#### Answer

Replace the forward slash (`/`) in `kubernetes.io/ingress.class` with `~1`.

Your command should look like this,

```rust
$ kubectl patch ingress mying --type='json' -p='[{"op": "add", "path": "/metadata/annotations/kubernetes.io~1ingress.class", "value":"nginx"}]'
```

Reference: [RFC 6901#Section-3](https://www.rfc-editor.org/rfc/rfc6901#section-3)

> A JSON Pointer is a Unicode string (see [RFC4627], Section 3) containing a sequence of zero or more reference tokens, each prefixed by a '/' (%x2F) character.
>
> Because the characters '~' (%x7E) and '/' (%x2F) have special meanings in JSON Pointer, '~' needs to be encoded as '~0' and '/' needs to be encoded as '~1' when these characters appear in a reference token.
