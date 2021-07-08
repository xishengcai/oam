module github.com/xishengcai/oam

go 1.16

require (
	github.com/crossplane/crossplane-runtime v0.11.0
	github.com/davecgh/go-spew v1.1.1
	github.com/go-logr/logr v0.1.0
	github.com/oam-dev/kubevela v0.3.4
	github.com/onsi/ginkgo v1.13.0
	github.com/onsi/gomega v1.10.3
	github.com/pkg/errors v0.9.1
	go.uber.org/zap v1.15.0
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
	istio.io/api v0.0.0-20201123152548-197f11e4ea09
	istio.io/client-go v1.8.1
	k8s.io/api v0.18.8
	k8s.io/apiextensions-apiserver v0.18.6
	k8s.io/apimachinery v0.18.8
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/klog v1.0.0
	k8s.io/kube-openapi v0.0.0-20200410145947-bcb3869e6f29
	k8s.io/kubectl v0.18.6
	sigs.k8s.io/controller-runtime v0.6.2
	sigs.k8s.io/controller-tools v0.2.4
)

replace k8s.io/client-go => k8s.io/client-go v0.18.8
