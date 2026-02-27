//go:build integration

package maas

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	kservev1alpha1 "github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	pollTimeout  = 15 * time.Second
	pollInterval = 200 * time.Millisecond
)

var (
	authPolicyGVK = schema.GroupVersionKind{Group: "kuadrant.io", Version: "v1", Kind: "AuthPolicy"}
	trlpGVK       = schema.GroupVersionKind{Group: "kuadrant.io", Version: "v1alpha1", Kind: "TokenRateLimitPolicy"}
)

type scenarioState struct {
	namespace string
}

func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: initializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("BDD tests failed")
	}
}

func initializeScenario(sc *godog.ScenarioContext) {
	s := &scenarioState{}

	sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "test-"},
		}
		if err := k8sClient.Create(testCtx, ns); err != nil {
			return ctx, fmt.Errorf("create test namespace: %w", err)
		}
		s.namespace = ns.Name
		return ctx, nil
	})

	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		if s.namespace == "" {
			return ctx, nil
		}
		nsOpt := client.InNamespace(s.namespace)
		// envtest has no namespace lifecycle controller, so we must explicitly
		// delete objects to prevent cross-namespace leaks in list operations.
		_ = k8sClient.DeleteAllOf(testCtx, &maasv1alpha1.MaaSAuthPolicy{}, nsOpt)
		_ = k8sClient.DeleteAllOf(testCtx, &maasv1alpha1.MaaSSubscription{}, nsOpt)
		_ = k8sClient.DeleteAllOf(testCtx, &maasv1alpha1.MaaSModel{}, nsOpt)
		// Wait for finalizer processing to complete (objects fully removed).
		_ = poll(pollTimeout, pollInterval, func() error {
			var policies maasv1alpha1.MaaSAuthPolicyList
			if listErr := k8sClient.List(testCtx, &policies, nsOpt); listErr == nil && len(policies.Items) > 0 {
				return fmt.Errorf("%d MaaSAuthPolicies still exist", len(policies.Items))
			}
			var subs maasv1alpha1.MaaSSubscriptionList
			if listErr := k8sClient.List(testCtx, &subs, nsOpt); listErr == nil && len(subs.Items) > 0 {
				return fmt.Errorf("%d MaaSSubscriptions still exist", len(subs.Items))
			}
			var models maasv1alpha1.MaaSModelList
			if listErr := k8sClient.List(testCtx, &models, nsOpt); listErr == nil && len(models.Items) > 0 {
				return fmt.Errorf("%d MaaSModels still exist", len(models.Items))
			}
			return nil
		})
		_ = k8sClient.Delete(testCtx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: s.namespace},
		})
		return ctx, nil
	})

	// Background
	sc.Step(`^a test namespace$`, s.aTestNamespace)
	sc.Step(`^a ready MaaSModel "([^"]*)" with an LLMInferenceService$`, s.readyModelWithLLMISvc)

	// LLMInferenceService
	sc.Step(`^an LLMInferenceService "([^"]*)" that is Ready$`, s.llmisvcReady)
	sc.Step(`^an LLMInferenceService "([^"]*)" that is not Ready$`, s.llmisvcNotReady)

	// MaaSModel
	sc.Step(`^a MaaSModel "([^"]*)" referencing LLMInferenceService "([^"]*)" is created$`, s.createMaaSModel)
	sc.Step(`^a MaaSModel "([^"]*)" referencing LLMInferenceService "([^"]*)"$`, s.createMaaSModel)
	sc.Step(`^the MaaSModel "([^"]*)" should reach phase "([^"]*)"$`, s.modelShouldReachPhase)
	sc.Step(`^the MaaSModel "([^"]*)" status should have an endpoint$`, s.modelShouldHaveEndpoint)
	sc.Step(`^the MaaSModel "([^"]*)" should have finalizer "([^"]*)"$`, s.modelShouldHaveFinalizer)
	sc.Step(`^the MaaSModel "([^"]*)" is Ready$`, s.waitModelReady)
	sc.Step(`^the MaaSModel "([^"]*)" is deleted$`, s.deleteMaaSModel)

	// MaaSAuthPolicy
	sc.Step(`^a MaaSAuthPolicy "([^"]*)" granting group "([^"]*)" access to model "([^"]*)" is created$`, s.createAuthPolicyForGroup)
	sc.Step(`^a MaaSAuthPolicy "([^"]*)" granting group "([^"]*)" access to model "([^"]*)"$`, s.createAuthPolicyForGroup)
	sc.Step(`^a MaaSAuthPolicy "([^"]*)" granting user "([^"]*)" access to model "([^"]*)" is created$`, s.createAuthPolicyForUser)
	sc.Step(`^a MaaSAuthPolicy "([^"]*)" granting user "([^"]*)" access to model "([^"]*)"$`, s.createAuthPolicyForUser)
	sc.Step(`^the MaaSAuthPolicy "([^"]*)" should reach phase "([^"]*)"$`, s.authPolicyShouldReachPhase)
	sc.Step(`^the MaaSAuthPolicy "([^"]*)" is Active$`, s.waitAuthPolicyActive)
	sc.Step(`^the MaaSAuthPolicy "([^"]*)" is deleted$`, s.deleteAuthPolicy)

	// MaaSSubscription
	sc.Step(`^a MaaSSubscription "([^"]*)" for group "([^"]*)" on model "([^"]*)" with limit (\d+) per "([^"]*)" is created$`, s.createSubscription)
	sc.Step(`^a MaaSSubscription "([^"]*)" for group "([^"]*)" on model "([^"]*)" with limit (\d+) per "([^"]*)"$`, s.createSubscription)
	sc.Step(`^the MaaSSubscription "([^"]*)" should reach phase "([^"]*)"$`, s.subscriptionShouldReachPhase)
	sc.Step(`^the MaaSSubscription "([^"]*)" is Active$`, s.waitSubscriptionActive)
	sc.Step(`^the MaaSSubscription "([^"]*)" is deleted$`, s.deleteSubscription)

	// Generated policy assertions
	sc.Step(`^an AuthPolicy "([^"]*)" should exist$`, s.authPolicyShouldExist)
	sc.Step(`^the AuthPolicy "([^"]*)" should not exist$`, s.authPolicyShouldNotExist)
	sc.Step(`^the AuthPolicy "([^"]*)" should have label "([^"]*)" with value "([^"]*)"$`, s.authPolicyShouldHaveLabel)
	sc.Step(`^the AuthPolicy "([^"]*)" annotation "([^"]*)" should contain "([^"]*)"$`, s.authPolicyAnnotationShouldContain)
	sc.Step(`^the AuthPolicy "([^"]*)" annotation "([^"]*)" should not contain "([^"]*)"$`, s.authPolicyAnnotationShouldNotContain)
	sc.Step(`^a TokenRateLimitPolicy "([^"]*)" should exist$`, s.trlpShouldExist)
	sc.Step(`^the TokenRateLimitPolicy "([^"]*)" should not exist$`, s.trlpShouldNotExist)
	sc.Step(`^the TokenRateLimitPolicy "([^"]*)" should have label "([^"]*)" with value "([^"]*)"$`, s.trlpShouldHaveLabel)
	sc.Step(`^the TokenRateLimitPolicy "([^"]*)" annotation "([^"]*)" should contain "([^"]*)"$`, s.trlpAnnotationShouldContain)
	sc.Step(`^the TokenRateLimitPolicy "([^"]*)" annotation "([^"]*)" should not contain "([^"]*)"$`, s.trlpAnnotationShouldNotContain)
}

// --- Helpers ---

func poll(timeout, interval time.Duration, fn func() error) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		time.Sleep(interval)
	}
	return fmt.Errorf("timed out after %s: %w", timeout, lastErr)
}

func (s *scenarioState) getUnstructured(gvk schema.GroupVersionKind, name string) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	err := k8sClient.Get(testCtx, client.ObjectKey{Name: name, Namespace: s.namespace}, obj)
	return obj, err
}

// --- Background ---

func (s *scenarioState) aTestNamespace() error {
	return nil // created in Before hook
}

func (s *scenarioState) readyModelWithLLMISvc(modelName string) error {
	if err := s.llmisvcReady(modelName); err != nil {
		return err
	}
	if err := s.createMaaSModel(modelName, modelName); err != nil {
		return err
	}
	return s.waitModelReady(modelName)
}

// --- LLMInferenceService ---

func (s *scenarioState) llmisvcReady(name string) error {
	llmisvc := &kservev1alpha1.LLMInferenceService{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace},
		Status: kservev1alpha1.LLMInferenceServiceStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{Type: "Ready", Status: corev1.ConditionTrue}},
			},
		},
	}
	return k8sClient.Create(testCtx, llmisvc)
}

func (s *scenarioState) llmisvcNotReady(name string) error {
	llmisvc := &kservev1alpha1.LLMInferenceService{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace},
		Status: kservev1alpha1.LLMInferenceServiceStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{Type: "Ready", Status: corev1.ConditionFalse}},
			},
		},
	}
	return k8sClient.Create(testCtx, llmisvc)
}

// --- MaaSModel ---

func (s *scenarioState) createMaaSModel(name, llmisvcName string) error {
	model := &maasv1alpha1.MaaSModel{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace},
		Spec: maasv1alpha1.MaaSModelSpec{
			ModelRef: maasv1alpha1.ModelReference{
				Kind: "LLMInferenceService",
				Name: llmisvcName,
			},
		},
	}
	return k8sClient.Create(testCtx, model)
}

func (s *scenarioState) modelShouldReachPhase(name, phase string) error {
	return poll(pollTimeout, pollInterval, func() error {
		m := &maasv1alpha1.MaaSModel{}
		if err := k8sClient.Get(testCtx, client.ObjectKey{Name: name, Namespace: s.namespace}, m); err != nil {
			return err
		}
		if m.Status.Phase != phase {
			return fmt.Errorf("MaaSModel %s phase = %q, want %q", name, m.Status.Phase, phase)
		}
		return nil
	})
}

func (s *scenarioState) modelShouldHaveEndpoint(name string) error {
	return poll(pollTimeout, pollInterval, func() error {
		m := &maasv1alpha1.MaaSModel{}
		if err := k8sClient.Get(testCtx, client.ObjectKey{Name: name, Namespace: s.namespace}, m); err != nil {
			return err
		}
		if m.Status.Endpoint == "" {
			return fmt.Errorf("MaaSModel %s has no endpoint", name)
		}
		return nil
	})
}

func (s *scenarioState) modelShouldHaveFinalizer(name, finalizer string) error {
	return poll(pollTimeout, pollInterval, func() error {
		m := &maasv1alpha1.MaaSModel{}
		if err := k8sClient.Get(testCtx, client.ObjectKey{Name: name, Namespace: s.namespace}, m); err != nil {
			return err
		}
		if !controllerutil.ContainsFinalizer(m, finalizer) {
			return fmt.Errorf("MaaSModel %s missing finalizer %q", name, finalizer)
		}
		return nil
	})
}

func (s *scenarioState) waitModelReady(name string) error {
	return s.modelShouldReachPhase(name, "Ready")
}

func (s *scenarioState) deleteMaaSModel(name string) error {
	return k8sClient.Delete(testCtx, &maasv1alpha1.MaaSModel{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace},
	})
}

// --- MaaSAuthPolicy ---

func (s *scenarioState) createAuthPolicyForGroup(name, group, model string) error {
	policy := &maasv1alpha1.MaaSAuthPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace},
		Spec: maasv1alpha1.MaaSAuthPolicySpec{
			ModelRefs: []string{model},
			Subjects: maasv1alpha1.SubjectSpec{
				Groups: []maasv1alpha1.GroupReference{{Name: group}},
			},
		},
	}
	return k8sClient.Create(testCtx, policy)
}

func (s *scenarioState) createAuthPolicyForUser(name, user, model string) error {
	policy := &maasv1alpha1.MaaSAuthPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace},
		Spec: maasv1alpha1.MaaSAuthPolicySpec{
			ModelRefs: []string{model},
			Subjects: maasv1alpha1.SubjectSpec{
				Users: []string{user},
			},
		},
	}
	return k8sClient.Create(testCtx, policy)
}

func (s *scenarioState) authPolicyShouldReachPhase(name, phase string) error {
	return poll(pollTimeout, pollInterval, func() error {
		p := &maasv1alpha1.MaaSAuthPolicy{}
		if err := k8sClient.Get(testCtx, client.ObjectKey{Name: name, Namespace: s.namespace}, p); err != nil {
			return err
		}
		if p.Status.Phase != phase {
			return fmt.Errorf("MaaSAuthPolicy %s phase = %q, want %q", name, p.Status.Phase, phase)
		}
		return nil
	})
}

func (s *scenarioState) waitAuthPolicyActive(name string) error {
	return s.authPolicyShouldReachPhase(name, "Active")
}

func (s *scenarioState) deleteAuthPolicy(name string) error {
	return k8sClient.Delete(testCtx, &maasv1alpha1.MaaSAuthPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace},
	})
}

// --- MaaSSubscription ---

func (s *scenarioState) createSubscription(name, group, model string, limit int, window string) error {
	sub := &maasv1alpha1.MaaSSubscription{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace},
		Spec: maasv1alpha1.MaaSSubscriptionSpec{
			Owner: maasv1alpha1.OwnerSpec{
				Groups: []maasv1alpha1.GroupReference{{Name: group}},
			},
			ModelRefs: []maasv1alpha1.ModelSubscriptionRef{{
				Name: model,
				TokenRateLimits: []maasv1alpha1.TokenRateLimit{
					{Limit: int64(limit), Window: window},
				},
			}},
		},
	}
	return k8sClient.Create(testCtx, sub)
}

func (s *scenarioState) subscriptionShouldReachPhase(name, phase string) error {
	return poll(pollTimeout, pollInterval, func() error {
		sub := &maasv1alpha1.MaaSSubscription{}
		if err := k8sClient.Get(testCtx, client.ObjectKey{Name: name, Namespace: s.namespace}, sub); err != nil {
			return err
		}
		if sub.Status.Phase != phase {
			return fmt.Errorf("MaaSSubscription %s phase = %q, want %q", name, sub.Status.Phase, phase)
		}
		return nil
	})
}

func (s *scenarioState) waitSubscriptionActive(name string) error {
	return s.subscriptionShouldReachPhase(name, "Active")
}

func (s *scenarioState) deleteSubscription(name string) error {
	return k8sClient.Delete(testCtx, &maasv1alpha1.MaaSSubscription{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace},
	})
}

// --- Generated AuthPolicy assertions ---

func (s *scenarioState) authPolicyShouldExist(name string) error {
	return poll(pollTimeout, pollInterval, func() error {
		_, err := s.getUnstructured(authPolicyGVK, name)
		return err
	})
}

func (s *scenarioState) authPolicyShouldNotExist(name string) error {
	return poll(pollTimeout, pollInterval, func() error {
		_, err := s.getUnstructured(authPolicyGVK, name)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("AuthPolicy %s still exists", name)
	})
}

func (s *scenarioState) authPolicyShouldHaveLabel(name, key, value string) error {
	return poll(pollTimeout, pollInterval, func() error {
		obj, err := s.getUnstructured(authPolicyGVK, name)
		if err != nil {
			return err
		}
		if obj.GetLabels()[key] != value {
			return fmt.Errorf("AuthPolicy %s label %q = %q, want %q", name, key, obj.GetLabels()[key], value)
		}
		return nil
	})
}

func (s *scenarioState) authPolicyAnnotationShouldContain(name, key, value string) error {
	return poll(pollTimeout, pollInterval, func() error {
		obj, err := s.getUnstructured(authPolicyGVK, name)
		if err != nil {
			return err
		}
		ann := obj.GetAnnotations()[key]
		if !strings.Contains(ann, value) {
			return fmt.Errorf("AuthPolicy %s annotation %q = %q, does not contain %q", name, key, ann, value)
		}
		return nil
	})
}

func (s *scenarioState) authPolicyAnnotationShouldNotContain(name, key, value string) error {
	return poll(pollTimeout, pollInterval, func() error {
		obj, err := s.getUnstructured(authPolicyGVK, name)
		if err != nil {
			return err // keep polling if not found (may be rebuilding)
		}
		ann := obj.GetAnnotations()[key]
		if strings.Contains(ann, value) {
			return fmt.Errorf("AuthPolicy %s annotation %q = %q, should not contain %q", name, key, ann, value)
		}
		return nil
	})
}

// --- Generated TokenRateLimitPolicy assertions ---

func (s *scenarioState) trlpShouldExist(name string) error {
	return poll(pollTimeout, pollInterval, func() error {
		_, err := s.getUnstructured(trlpGVK, name)
		return err
	})
}

func (s *scenarioState) trlpShouldNotExist(name string) error {
	return poll(pollTimeout, pollInterval, func() error {
		_, err := s.getUnstructured(trlpGVK, name)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("TokenRateLimitPolicy %s still exists", name)
	})
}

func (s *scenarioState) trlpShouldHaveLabel(name, key, value string) error {
	return poll(pollTimeout, pollInterval, func() error {
		obj, err := s.getUnstructured(trlpGVK, name)
		if err != nil {
			return err
		}
		if obj.GetLabels()[key] != value {
			return fmt.Errorf("TRLP %s label %q = %q, want %q", name, key, obj.GetLabels()[key], value)
		}
		return nil
	})
}

func (s *scenarioState) trlpAnnotationShouldContain(name, key, value string) error {
	return poll(pollTimeout, pollInterval, func() error {
		obj, err := s.getUnstructured(trlpGVK, name)
		if err != nil {
			return err
		}
		ann := obj.GetAnnotations()[key]
		if !strings.Contains(ann, value) {
			return fmt.Errorf("TRLP %s annotation %q = %q, does not contain %q", name, key, ann, value)
		}
		return nil
	})
}

func (s *scenarioState) trlpAnnotationShouldNotContain(name, key, value string) error {
	return poll(pollTimeout, pollInterval, func() error {
		obj, err := s.getUnstructured(trlpGVK, name)
		if err != nil {
			return err // keep polling if not found (may be rebuilding)
		}
		ann := obj.GetAnnotations()[key]
		if strings.Contains(ann, value) {
			return fmt.Errorf("TRLP %s annotation %q = %q, should not contain %q", name, key, ann, value)
		}
		return nil
	})
}
