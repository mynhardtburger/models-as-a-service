//go:build integration

package maas

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	kservev1alpha1 "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	"github.com/opendatahub-io/models-as-a-service/maas-controller/pkg/controller/maas/testutil"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	testEnv    *envtest.Environment
	k8sClient  client.Client
	testCtx    context.Context
	testCancel context.CancelFunc
)

const (
	testGatewayName = "test-gateway"
	testGatewayNS   = "test-gateway-ns"
)

func TestMain(m *testing.M) {
	testScheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(testScheme))
	utilruntime.Must(gatewayapiv1.Install(testScheme))
	utilruntime.Must(maasv1alpha1.AddToScheme(testScheme))
	utilruntime.Must(kservev1alpha1.AddToScheme(testScheme))

	testCtx, testCancel = context.WithCancel(context.Background())

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd", "bases"),
			filepath.Join("testdata", "crd"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start envtest: %v\n", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  testScheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create manager: %v\n", err)
		os.Exit(1)
	}

	// Register stub controllers
	setupFns := []func() error{
		func() error {
			return (&testutil.FakeKServeController{
				Client:           mgr.GetClient(),
				Scheme:           testScheme,
				GatewayName:      testGatewayName,
				GatewayNamespace: testGatewayNS,
			}).SetupWithManager(mgr)
		},
		func() error {
			return (&testutil.FakeKuadrantAuthPolicyController{
				Client: mgr.GetClient(),
			}).SetupWithManager(mgr)
		},
		func() error {
			return (&testutil.FakeKuadrantTRLPController{
				Client: mgr.GetClient(),
			}).SetupWithManager(mgr)
		},
		// Register real controllers
		func() error {
			return (&MaaSModelReconciler{
				Client:           mgr.GetClient(),
				Scheme:           testScheme,
				GatewayName:      testGatewayName,
				GatewayNamespace: testGatewayNS,
			}).SetupWithManager(mgr)
		},
		func() error {
			return (&MaaSAuthPolicyReconciler{
				Client:           mgr.GetClient(),
				Scheme:           testScheme,
				MaaSAPINamespace: "default",
				GatewayName:      testGatewayName,
				ClusterAudience:  "https://kubernetes.default.svc",
			}).SetupWithManager(mgr)
		},
		func() error {
			return (&MaaSSubscriptionReconciler{
				Client: mgr.GetClient(),
				Scheme: testScheme,
			}).SetupWithManager(mgr)
		},
	}

	for _, fn := range setupFns {
		if err := fn(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to setup controller: %v\n", err)
			testCancel()
			_ = testEnv.Stop()
			os.Exit(1)
		}
	}

	k8sClient = mgr.GetClient()

	go func() {
		if err := mgr.Start(testCtx); err != nil {
			fmt.Fprintf(os.Stderr, "manager error: %v\n", err)
		}
	}()

	code := m.Run()

	testCancel()
	_ = testEnv.Stop()
	os.Exit(code)
}
