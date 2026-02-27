package testutil

import (
	"context"

	kservev1alpha1 "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// FakeKServeController simulates KServe by creating an HTTPRoute with the
// expected labels whenever an LLMInferenceService is created or updated.
type FakeKServeController struct {
	client.Client
	Scheme           *runtime.Scheme
	GatewayName      string
	GatewayNamespace string
}

func (r *FakeKServeController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	llmisvc := &kservev1alpha1.LLMInferenceService{}
	if err := r.Get(ctx, req.NamespacedName, llmisvc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	routeName := llmisvc.Name + "-route"

	// Check if HTTPRoute already exists
	existing := &gatewayapiv1.HTTPRoute{}
	err := r.Get(ctx, client.ObjectKey{Name: routeName, Namespace: llmisvc.Namespace}, existing)
	if err == nil {
		return ctrl.Result{}, nil
	}
	if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	// Create HTTPRoute with labels that llmisvcHandler expects
	gwNS := gatewayapiv1.Namespace(r.GatewayNamespace)
	route := &gatewayapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: llmisvc.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      llmisvc.Name,
				"app.kubernetes.io/component": "llminferenceservice-router",
				"app.kubernetes.io/part-of":   "llminferenceservice",
			},
		},
		Spec: gatewayapiv1.HTTPRouteSpec{
			Hostnames: []gatewayapiv1.Hostname{
				gatewayapiv1.Hostname(llmisvc.Name + ".models.example.com"),
			},
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: []gatewayapiv1.ParentReference{{
					Name:      gatewayapiv1.ObjectName(r.GatewayName),
					Namespace: &gwNS,
				}},
			},
		},
	}

	return ctrl.Result{}, r.Create(ctx, route)
}

func (r *FakeKServeController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kservev1alpha1.LLMInferenceService{}).
		Complete(r)
}
