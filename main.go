package main

import (
	"flag"
	"os"
	"strconv"
	"time"

	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/xishengcai/oam/apis/core"
	"github.com/xishengcai/oam/pkg/controller"
	appController "github.com/xishengcai/oam/pkg/controller/v1alpha2"
	webhook "github.com/xishengcai/oam/pkg/webhook/v1alpha2"
)

var scheme = runtime.NewScheme()

func setup() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = core.AddToScheme(scheme)
	_ = crdv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr, leaderElectionNamespace string
	var enableLeaderElection bool
	var certDir string
	var webhookPort int
	var useWebhook bool
	var controllerArgs = controller.Args{}

	flag.BoolVar(&useWebhook, "use-webhook", false, "Enable Admission Webhook")
	flag.StringVar(&certDir, "webhook-cert-dir", "/k8s-webhook-server/serving-certs", "Admission webhook cert/key dir.")
	flag.IntVar(&webhookPort, "webhook-port", 9443, "admission webhook listen address")
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&leaderElectionNamespace, "leader-election-namespace", "",
		"Determines the namespace in which the leader election configmap will be created.")
	flag.IntVar(&controllerArgs.RevisionLimit, "revision-limit", 50,
		"RevisionLimit is the maximum number of revisions that will be maintained. The default value is 50.")
	flag.BoolVar(&controllerArgs.ApplyOnceOnly, "apply-once-only", false,
		"For the purpose of some production environment that workload or trait should not be affected if no spec change")
	flag.DurationVar(&controllerArgs.SyncTime, "sync-time", 180*time.Second, "Set Reconcile exec interval,unit is second")
	flag.Parse()

	// use set replace init
	setup()

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		MetricsBindAddress:      metricsAddr,
		LeaderElection:          enableLeaderElection,
		LeaderElectionID:        "oam-kubernetes-runtime",
		LeaderElectionNamespace: leaderElectionNamespace,
		Port:                    webhookPort,
		CertDir:                 certDir,
	})
	if err != nil {
		klog.Error(err, "unable to create a controller manager")
		os.Exit(1)
	}

	if useWebhook {
		klog.Infof("OAM webhook enabled, will serving at : %s", strconv.Itoa(webhookPort))
		if err = webhook.Add(mgr); err != nil {
			klog.Error(err, "unable to setup the webhook for core controller")
			os.Exit(1)
		}
	}

	if err = appController.Setup(mgr, controllerArgs); err != nil {
		klog.Error("unable to setup the oam core controller", err)
		os.Exit(1)
	}
	klog.Info("starting the controller manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
