package cmd

import (
	"fmt"
	"os"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	configsv1 "github.com/flanksource/apm-hub/api/v1"
	"github.com/flanksource/apm-hub/controllers"
	"github.com/flanksource/commons/logger"
	"github.com/go-logr/zapr"
	"github.com/spf13/cobra"
)

var webhookPort int
var enableLeaderElection bool
var operatorExecutor bool
var Operator = &cobra.Command{
	Use:   "operator",
	Short: "Start the kubernetes operator",
	Run:   run,
}

func init() {
	ServerFlags(Operator.Flags())
	Operator.Flags().BoolVar(&operatorExecutor, "executor", true, "If false, only serve the UI and sync the configs")
	Operator.Flags().IntVar(&webhookPort, "webhookPort", 8082, "Port for webhooks ")
	Operator.Flags().BoolVar(&enableLeaderElection, "enable-leader-election", false, "Enabling this will ensure there is only one active controller manager")
}

func run(cmd *cobra.Command, args []string) {
	zapLogger := logger.GetZapLogger()
	if zapLogger == nil {
		logger.Fatalf("failed to get zap logger")
		return
	}

	loggr := ctrlzap.NewRaw(
		ctrlzap.UseDevMode(true),
		ctrlzap.WriteTo(os.Stderr),
		ctrlzap.Level(zapLogger.Level),
		ctrlzap.StacktraceLevel(zapLogger.StackTraceLevel),
		ctrlzap.Encoder(zapLogger.GetEncoder()),
	)

	ctrl.SetLogger(zapr.NewLogger(loggr))
	setupLog := ctrl.Log.WithName("setup")

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(configsv1.AddToScheme(scheme))

	// Start the server
	go runServe(nil, args)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: fmt.Sprintf("0.0.0.0:%d", metricsPort),
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "1df594fc.flanksource.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.LoggingBackendReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Log:    ctrl.Log.WithName("controllers").WithName("apmhub_config"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "LoggingBackend")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
