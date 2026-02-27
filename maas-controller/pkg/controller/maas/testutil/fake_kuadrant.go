package testutil

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FakeKuadrantAuthPolicyController stamps Accepted/Enforced status conditions
// on AuthPolicy objects, simulating Kuadrant operator behavior.
type FakeKuadrantAuthPolicyController struct {
	client.Client
}

func (r *FakeKuadrantAuthPolicyController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ap := &unstructured.Unstructured{}
	ap.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "kuadrant.io", Version: "v1", Kind: "AuthPolicy",
	})
	if err := r.Get(ctx, req.NamespacedName, ap); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip if conditions already stamped
	conditions, _, _ := unstructured.NestedSlice(ap.Object, "status", "conditions")
	for _, c := range conditions {
		if cm, ok := c.(map[string]interface{}); ok {
			if cm["type"] == "Accepted" && cm["status"] == "True" {
				return ctrl.Result{}, nil
			}
		}
	}

	now := metav1.Now().UTC().Format(time.RFC3339)
	newConditions := []interface{}{
		map[string]interface{}{
			"type": "Accepted", "status": "True",
			"reason": "Accepted", "message": "Policy accepted",
			"lastTransitionTime": now,
		},
		map[string]interface{}{
			"type": "Enforced", "status": "True",
			"reason": "Enforced", "message": "Policy enforced",
			"lastTransitionTime": now,
		},
	}
	if err := unstructured.SetNestedSlice(ap.Object, newConditions, "status", "conditions"); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, r.Status().Update(ctx, ap)
}

func (r *FakeKuadrantAuthPolicyController) SetupWithManager(mgr ctrl.Manager) error {
	ap := &unstructured.Unstructured{}
	ap.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "kuadrant.io", Version: "v1", Kind: "AuthPolicy",
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(ap).
		Complete(r)
}

// FakeKuadrantTRLPController stamps Accepted/Enforced status conditions
// on TokenRateLimitPolicy objects, simulating Kuadrant operator behavior.
type FakeKuadrantTRLPController struct {
	client.Client
}

func (r *FakeKuadrantTRLPController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	trlp := &unstructured.Unstructured{}
	trlp.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "kuadrant.io", Version: "v1alpha1", Kind: "TokenRateLimitPolicy",
	})
	if err := r.Get(ctx, req.NamespacedName, trlp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	conditions, _, _ := unstructured.NestedSlice(trlp.Object, "status", "conditions")
	for _, c := range conditions {
		if cm, ok := c.(map[string]interface{}); ok {
			if cm["type"] == "Accepted" && cm["status"] == "True" {
				return ctrl.Result{}, nil
			}
		}
	}

	now := metav1.Now().UTC().Format(time.RFC3339)
	newConditions := []interface{}{
		map[string]interface{}{
			"type": "Accepted", "status": "True",
			"reason": "Accepted", "message": "Policy accepted",
			"lastTransitionTime": now,
		},
		map[string]interface{}{
			"type": "Enforced", "status": "True",
			"reason": "Enforced", "message": "Policy enforced",
			"lastTransitionTime": now,
		},
	}
	if err := unstructured.SetNestedSlice(trlp.Object, newConditions, "status", "conditions"); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, r.Status().Update(ctx, trlp)
}

func (r *FakeKuadrantTRLPController) SetupWithManager(mgr ctrl.Manager) error {
	trlp := &unstructured.Unstructured{}
	trlp.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "kuadrant.io", Version: "v1alpha1", Kind: "TokenRateLimitPolicy",
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(trlp).
		Complete(r)
}
