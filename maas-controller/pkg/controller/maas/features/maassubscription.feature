Feature: MaaSSubscription creates TokenRateLimitPolicies
  The MaaSSubscription controller aggregates all subscriptions for a model
  into a single Kuadrant TokenRateLimitPolicy.

  Background:
    Given a test namespace
    And a ready MaaSModel "llama" with an LLMInferenceService

  Scenario: Subscription creates a TokenRateLimitPolicy
    When a MaaSSubscription "basic" for group "users" on model "llama" with limit 100 per "1m" is created
    Then the MaaSSubscription "basic" should reach phase "Active"
    And a TokenRateLimitPolicy "maas-trlp-llama" should exist
    And the TokenRateLimitPolicy "maas-trlp-llama" should have label "maas.opendatahub.io/model" with value "llama"

  Scenario: Multiple subscriptions are aggregated into one TRLP
    Given a MaaSSubscription "basic" for group "users" on model "llama" with limit 100 per "1m"
    And the MaaSSubscription "basic" is Active
    When a MaaSSubscription "premium" for group "premium" on model "llama" with limit 10000 per "1m" is created
    Then the MaaSSubscription "premium" should reach phase "Active"
    And the TokenRateLimitPolicy "maas-trlp-llama" annotation "maas.opendatahub.io/subscriptions" should contain "basic"
    And the TokenRateLimitPolicy "maas-trlp-llama" annotation "maas.opendatahub.io/subscriptions" should contain "premium"

  Scenario: Deleting a subscription rebuilds the TRLP
    Given a MaaSSubscription "basic" for group "users" on model "llama" with limit 100 per "1m"
    And a MaaSSubscription "premium" for group "premium" on model "llama" with limit 10000 per "1m"
    And the MaaSSubscription "basic" is Active
    And the MaaSSubscription "premium" is Active
    When the MaaSSubscription "basic" is deleted
    Then the TokenRateLimitPolicy "maas-trlp-llama" annotation "maas.opendatahub.io/subscriptions" should contain "premium"
    And the TokenRateLimitPolicy "maas-trlp-llama" annotation "maas.opendatahub.io/subscriptions" should not contain "basic"
