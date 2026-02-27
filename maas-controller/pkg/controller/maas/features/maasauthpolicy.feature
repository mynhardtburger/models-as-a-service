Feature: MaaSAuthPolicy creates Kuadrant AuthPolicies
  The MaaSAuthPolicy controller aggregates all auth policies for a model
  into a single Kuadrant AuthPolicy.

  Background:
    Given a test namespace
    And a ready MaaSModel "llama" with an LLMInferenceService

  Scenario: Auth policy for a group creates an AuthPolicy
    When a MaaSAuthPolicy "team-a-access" granting group "team-a" access to model "llama" is created
    Then the MaaSAuthPolicy "team-a-access" should reach phase "Active"
    And an AuthPolicy "maas-auth-llama" should exist
    And the AuthPolicy "maas-auth-llama" should have label "maas.opendatahub.io/model" with value "llama"
    And the AuthPolicy "maas-auth-llama" should have label "app.kubernetes.io/managed-by" with value "maas-controller"

  Scenario: Auth policy for a user creates an AuthPolicy
    When a MaaSAuthPolicy "user-access" granting user "alice" access to model "llama" is created
    Then the MaaSAuthPolicy "user-access" should reach phase "Active"
    And an AuthPolicy "maas-auth-llama" should exist

  Scenario: Multiple auth policies are aggregated into one AuthPolicy
    Given a MaaSAuthPolicy "team-a-access" granting group "team-a" access to model "llama"
    And the MaaSAuthPolicy "team-a-access" is Active
    When a MaaSAuthPolicy "team-b-access" granting group "team-b" access to model "llama" is created
    Then the MaaSAuthPolicy "team-b-access" should reach phase "Active"
    And the AuthPolicy "maas-auth-llama" annotation "maas.opendatahub.io/auth-policies" should contain "team-a-access"
    And the AuthPolicy "maas-auth-llama" annotation "maas.opendatahub.io/auth-policies" should contain "team-b-access"

  Scenario: Deleting an auth policy rebuilds the aggregated AuthPolicy
    Given a MaaSAuthPolicy "team-a-access" granting group "team-a" access to model "llama"
    And a MaaSAuthPolicy "team-b-access" granting group "team-b" access to model "llama"
    And the MaaSAuthPolicy "team-a-access" is Active
    And the MaaSAuthPolicy "team-b-access" is Active
    When the MaaSAuthPolicy "team-a-access" is deleted
    Then the AuthPolicy "maas-auth-llama" annotation "maas.opendatahub.io/auth-policies" should contain "team-b-access"
    And the AuthPolicy "maas-auth-llama" annotation "maas.opendatahub.io/auth-policies" should not contain "team-a-access"
