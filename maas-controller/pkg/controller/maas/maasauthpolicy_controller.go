/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package maas

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// MaaSAuthPolicyReconciler reconciles a MaaSAuthPolicy object
type MaaSAuthPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=maas.opendatahub.io,resources=maasauthpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=maas.opendatahub.io,resources=maasauthpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=maas.opendatahub.io,resources=maasauthpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=maas.opendatahub.io,resources=maasmodels,verbs=get;list;watch
//+kubebuilder:rbac:groups=kuadrant.io,resources=authpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *MaaSAuthPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logr.FromContextOrDiscard(ctx).WithValues("MaaSAuthPolicy", req.NamespacedName)

	policy := &maasv1alpha1.MaaSAuthPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch MaaSAuthPolicy")
		return ctrl.Result{}, err
	}

	if !policy.GetDeletionTimestamp().IsZero() {
		return r.handleDeletion(ctx, log, policy)
	}

	refs, err := r.reconcileModelAuthPolicies(ctx, log, policy)
	if err != nil {
		log.Error(err, "failed to reconcile model AuthPolicies")
		r.updateStatus(ctx, policy, "Failed", fmt.Sprintf("Failed to reconcile: %v", err))
		return ctrl.Result{}, err
	}

	r.updateAuthPolicyRefStatus(ctx, log, policy, refs)
	r.updateStatus(ctx, policy, "Active", "Successfully reconciled")
	return ctrl.Result{}, nil
}

type authPolicyRef struct {
	Name      string
	Namespace string
	Model     string
}

func (r *MaaSAuthPolicyReconciler) reconcileModelAuthPolicies(ctx context.Context, log logr.Logger, policy *maasv1alpha1.MaaSAuthPolicy) ([]authPolicyRef, error) {
	var refs []authPolicyRef
	for _, modelName := range policy.Spec.ModelRefs {
		httpRouteName, httpRouteNS, err := r.findHTTPRouteForModel(ctx, log, policy.Namespace, modelName)
		if err != nil {
			log.Error(err, "failed to find HTTPRoute for model", "model", modelName)
			return nil, fmt.Errorf("failed to find HTTPRoute for model %s: %w", modelName, err)
		}
		refs = append(refs, authPolicyRef{
			Name:      fmt.Sprintf("maas-auth-%s-model-%s", policy.Name, modelName),
			Namespace: httpRouteNS,
			Model:     modelName,
		})

		authPolicyName := fmt.Sprintf("maas-auth-%s-model-%s", policy.Name, modelName)
		authPolicy := &unstructured.Unstructured{}
		authPolicy.SetGroupVersionKind(schema.GroupVersionKind{Group: "kuadrant.io", Version: "v1", Kind: "AuthPolicy"})
		authPolicy.SetName(authPolicyName)
		authPolicy.SetNamespace(httpRouteNS)

		labels := map[string]string{
			"maas.opendatahub.io/auth-policy":    policy.Name,
			"maas.opendatahub.io/auth-policy-ns": policy.Namespace,
			"app.kubernetes.io/managed-by":       "maas-controller",
			"app.kubernetes.io/part-of":          "maas-auth-policy",
			"app.kubernetes.io/component":        "auth-policy",
		}
		authPolicy.SetLabels(labels)

		if httpRouteNS == policy.Namespace {
			if err := controllerutil.SetControllerReference(policy, authPolicy, r.Scheme); err != nil {
				return nil, fmt.Errorf("failed to set controller reference: %w", err)
			}
		}

		var groupNames []string
		for _, group := range policy.Spec.Subjects.Groups {
			groupNames = append(groupNames, group.Name)
		}
		audiences := []interface{}{"maas-default-gateway-sa", "https://kubernetes.default.svc"}

		rule := map[string]interface{}{
			"authentication": map[string]interface{}{
				"service-accounts": map[string]interface{}{
					"cache": map[string]interface{}{
						"key": map[string]interface{}{"selector": "context.request.http.headers.authorization.@case:lower"},
						"ttl": int64(600),
					},
					"defaults": map[string]interface{}{
						"userid": map[string]interface{}{"selector": "auth.identity.user.username"},
					},
					"kubernetesTokenReview": map[string]interface{}{"audiences": audiences},
					"metrics":               false, "priority": int64(0),
				},
			},
		}

		var exclConditions []interface{}
		for _, g := range groupNames {
			exclConditions = append(exclConditions, map[string]interface{}{
				"operator": "excl", "selector": "auth.identity.user.groups", "value": g,
			})
		}
		denyWhen := []interface{}{map[string]interface{}{"all": exclConditions}}
		if len(exclConditions) == 0 {
			denyWhen = []interface{}{}
		}
		rule["authorization"] = map[string]interface{}{
			"deny-not-in-allowed-groups": map[string]interface{}{
				"metrics": false,
				"patternMatching": map[string]interface{}{
					"patterns": []interface{}{map[string]interface{}{
						"operator": "eq", "selector": "auth.identity.user.username", "value": "",
					}},
				},
				"priority": int64(0), "when": denyWhen,
			},
		}

		groupsFilterExpr := "auth.identity.user.groups"
		if len(groupNames) > 0 {
			groupsFilterExpr = "auth.identity.user.groups.filter(g, " + groupOrExpr(groupNames) + ")"
		}
		rule["response"] = map[string]interface{}{
			"success": map[string]interface{}{
				"filters": map[string]interface{}{
					"identity": map[string]interface{}{
						"json": map[string]interface{}{
							"properties": map[string]interface{}{
								"groups": map[string]interface{}{"expression": groupsFilterExpr},
								"userid": map[string]interface{}{
									"expression": "auth.identity.user.username", "selector": "auth.identity.userid",
								},
							},
						},
						"metrics": true, "priority": int64(0),
					},
				},
			},
		}
		spec := map[string]interface{}{
			"targetRef": map[string]interface{}{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute", "name": httpRouteName},
			"rules":     rule,
		}
		if err := unstructured.SetNestedMap(authPolicy.Object, spec, "spec"); err != nil {
			return nil, fmt.Errorf("failed to set spec: %w", err)
		}

		if policy.Spec.MeteringMetadata != nil {
			annotations := make(map[string]string)
			if policy.Spec.MeteringMetadata.OrganizationID != "" {
				annotations["maas.opendatahub.io/organization-id"] = policy.Spec.MeteringMetadata.OrganizationID
			}
			if policy.Spec.MeteringMetadata.CostCenter != "" {
				annotations["maas.opendatahub.io/cost-center"] = policy.Spec.MeteringMetadata.CostCenter
			}
			for k, v := range policy.Spec.MeteringMetadata.Labels {
				annotations[fmt.Sprintf("maas.opendatahub.io/label/%s", k)] = v
			}
			authPolicy.SetAnnotations(annotations)
		}

		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(authPolicy.GroupVersionKind())
		key := client.ObjectKeyFromObject(authPolicy)
		err = r.Get(ctx, key, existing)
		if apierrors.IsNotFound(err) {
			if err := r.Create(ctx, authPolicy); err != nil {
				return nil, fmt.Errorf("failed to create AuthPolicy for model %s: %w", modelName, err)
			}
			log.Info("AuthPolicy created", "name", authPolicyName, "model", modelName, "httpRoute", httpRouteName, "namespace", httpRouteNS)
		} else if err != nil {
			return nil, fmt.Errorf("failed to get existing AuthPolicy: %w", err)
		} else {
			existing.SetAnnotations(authPolicy.GetAnnotations())
			existing.SetLabels(authPolicy.GetLabels())
			if httpRouteNS == policy.Namespace {
				if err := controllerutil.SetControllerReference(policy, existing, r.Scheme); err != nil {
					return nil, fmt.Errorf("failed to update controller reference: %w", err)
				}
			}
			if err := unstructured.SetNestedMap(existing.Object, spec, "spec"); err != nil {
				return nil, fmt.Errorf("failed to update spec: %w", err)
			}
			if err := r.Update(ctx, existing); err != nil {
				return nil, fmt.Errorf("failed to update AuthPolicy for model %s: %w", modelName, err)
			}
			log.Info("AuthPolicy updated", "name", authPolicyName, "model", modelName, "httpRoute", httpRouteName, "namespace", httpRouteNS)
		}
	}
	return refs, nil
}

func groupOrExpr(groupNames []string) string {
	parts := make([]string, len(groupNames))
	for i, g := range groupNames {
		parts[i] = fmt.Sprintf(`g == %q`, g)
	}
	return strings.Join(parts, " || ")
}

func (r *MaaSAuthPolicyReconciler) findHTTPRouteForModel(ctx context.Context, log logr.Logger, defaultNS, modelName string) (string, string, error) {
	maasModelList := &maasv1alpha1.MaaSModelList{}
	if err := r.List(ctx, maasModelList); err != nil {
		return "", "", fmt.Errorf("failed to list MaaSModels: %w", err)
	}
	var maasModel *maasv1alpha1.MaaSModel
	for i := range maasModelList.Items {
		if maasModelList.Items[i].Name == modelName {
			if maasModelList.Items[i].Namespace == defaultNS {
				maasModel = &maasModelList.Items[i]
				break
			}
			if maasModel == nil {
				maasModel = &maasModelList.Items[i]
			}
		}
	}
	if maasModel == nil {
		return "", "", fmt.Errorf("MaaSModel %s not found", modelName)
	}
	var httpRouteName string
	httpRouteNS := maasModel.Namespace
	if maasModel.Spec.ModelRef.Namespace != "" {
		httpRouteNS = maasModel.Spec.ModelRef.Namespace
	}
	switch maasModel.Spec.ModelRef.Kind {
	case "llmisvc":
		llmisvcNS := maasModel.Namespace
		if maasModel.Spec.ModelRef.Namespace != "" {
			llmisvcNS = maasModel.Spec.ModelRef.Namespace
		}
		routeList := &gatewayapiv1.HTTPRouteList{}
		labelSelector := client.MatchingLabels{
			"app.kubernetes.io/name":      maasModel.Spec.ModelRef.Name,
			"app.kubernetes.io/component": "llminferenceservice-router",
			"app.kubernetes.io/part-of":   "llminferenceservice",
		}
		if err := r.List(ctx, routeList, client.InNamespace(llmisvcNS), labelSelector); err != nil {
			return "", "", fmt.Errorf("failed to list HTTPRoutes for LLMInferenceService %s: %w", maasModel.Spec.ModelRef.Name, err)
		}
		if len(routeList.Items) == 0 {
			return "", "", fmt.Errorf("HTTPRoute not found for LLMInferenceService %s in namespace %s", maasModel.Spec.ModelRef.Name, llmisvcNS)
		}
		httpRouteName = routeList.Items[0].Name
		httpRouteNS = routeList.Items[0].Namespace
	case "ExternalModel":
		httpRouteName = fmt.Sprintf("maas-model-%s", maasModel.Name)
	default:
		return "", "", fmt.Errorf("unknown model kind: %s", maasModel.Spec.ModelRef.Kind)
	}
	httpRoute := &gatewayapiv1.HTTPRoute{}
	key := client.ObjectKey{Name: httpRouteName, Namespace: httpRouteNS}
	if err := r.Get(ctx, key, httpRoute); err != nil {
		if apierrors.IsNotFound(err) {
			return "", "", fmt.Errorf("HTTPRoute %s/%s not found for model %s", httpRouteNS, httpRouteName, modelName)
		}
		return "", "", fmt.Errorf("failed to get HTTPRoute %s/%s: %w", httpRouteNS, httpRouteName, err)
	}
	return httpRouteName, httpRouteNS, nil
}

func (r *MaaSAuthPolicyReconciler) handleDeletion(ctx context.Context, log logr.Logger, policy *maasv1alpha1.MaaSAuthPolicy) (ctrl.Result, error) {
	for _, modelName := range policy.Spec.ModelRefs {
		_, httpRouteNS, err := r.findHTTPRouteForModel(ctx, log, policy.Namespace, modelName)
		if err != nil {
			log.Info("failed to find HTTPRoute for model during deletion, trying policy namespace", "model", modelName, "error", err)
			httpRouteNS = policy.Namespace
		}
		authPolicyName := fmt.Sprintf("maas-auth-%s-model-%s", policy.Name, modelName)
		authPolicy := &unstructured.Unstructured{}
		authPolicy.SetGroupVersionKind(schema.GroupVersionKind{Group: "kuadrant.io", Version: "v1", Kind: "AuthPolicy"})
		authPolicy.SetName(authPolicyName)
		authPolicy.SetNamespace(httpRouteNS)
		if err := r.Delete(ctx, authPolicy); err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "failed to delete AuthPolicy", "name", authPolicyName, "namespace", httpRouteNS)
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *MaaSAuthPolicyReconciler) updateAuthPolicyRefStatus(ctx context.Context, log logr.Logger, policy *maasv1alpha1.MaaSAuthPolicy, refs []authPolicyRef) {
	policy.Status.AuthPolicies = make([]maasv1alpha1.AuthPolicyRefStatus, 0, len(refs))
	for _, ref := range refs {
		ap := &unstructured.Unstructured{}
		ap.SetGroupVersionKind(schema.GroupVersionKind{Group: "kuadrant.io", Version: "v1", Kind: "AuthPolicy"})
		ap.SetNamespace(ref.Namespace)
		ap.SetName(ref.Name)
		if err := r.Get(ctx, client.ObjectKeyFromObject(ap), ap); err != nil {
			log.Info("could not get AuthPolicy for status", "name", ref.Name, "namespace", ref.Namespace, "error", err)
			policy.Status.AuthPolicies = append(policy.Status.AuthPolicies, maasv1alpha1.AuthPolicyRefStatus{
				Name: ref.Name, Namespace: ref.Namespace, Model: ref.Model, Accepted: "Unknown", Enforced: "Unknown",
			})
			continue
		}
		accepted, enforced := getAuthPolicyConditionState(ap)
		policy.Status.AuthPolicies = append(policy.Status.AuthPolicies, maasv1alpha1.AuthPolicyRefStatus{
			Name: ref.Name, Namespace: ref.Namespace, Model: ref.Model, Accepted: accepted, Enforced: enforced,
		})
	}
}

func getAuthPolicyConditionState(ap *unstructured.Unstructured) (accepted, enforced string) {
	accepted, enforced = "Unknown", "Unknown"
	conditions, found, err := unstructured.NestedSlice(ap.Object, "status", "conditions")
	if err != nil || !found || len(conditions) == 0 {
		return accepted, enforced
	}
	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := cond["type"].(string)
		status, _ := cond["status"].(string)
		switch typ {
		case "Accepted":
			accepted = status
		case "Enforced":
			enforced = status
		}
	}
	return accepted, enforced
}

func (r *MaaSAuthPolicyReconciler) updateStatus(ctx context.Context, policy *maasv1alpha1.MaaSAuthPolicy, phase, message string) {
	policy.Status.Phase = phase
	condition := metav1.Condition{
		Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled", Message: message, LastTransitionTime: metav1.Now(),
	}
	if phase == "Failed" {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ReconcileFailed"
	}
	found := false
	for i, c := range policy.Status.Conditions {
		if c.Type == condition.Type {
			policy.Status.Conditions[i] = condition
			found = true
			break
		}
	}
	if !found {
		policy.Status.Conditions = append(policy.Status.Conditions, condition)
	}
	r.Status().Update(ctx, policy)
}

func (r *MaaSAuthPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&maasv1alpha1.MaaSAuthPolicy{}).
		Complete(r)
}
