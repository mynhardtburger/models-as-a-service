Feature: MaaSModel lifecycle
  The MaaSModel controller validates backing LLMInferenceService and
  HTTPRoute resources and sets the model phase accordingly.

  Background:
    Given a test namespace

  Scenario: Model becomes Ready when LLMInferenceService is Ready
    Given an LLMInferenceService "my-llm" that is Ready
    When a MaaSModel "my-model" referencing LLMInferenceService "my-llm" is created
    Then the MaaSModel "my-model" should reach phase "Ready"
    And the MaaSModel "my-model" status should have an endpoint

  Scenario: Model stays Pending when LLMInferenceService is not Ready
    Given an LLMInferenceService "my-llm" that is not Ready
    When a MaaSModel "my-model" referencing LLMInferenceService "my-llm" is created
    Then the MaaSModel "my-model" should reach phase "Pending"

  Scenario: Model has cleanup finalizer
    Given an LLMInferenceService "my-llm" that is Ready
    When a MaaSModel "my-model" referencing LLMInferenceService "my-llm" is created
    Then the MaaSModel "my-model" should reach phase "Ready"
    And the MaaSModel "my-model" should have finalizer "maas.opendatahub.io/model-cleanup"
