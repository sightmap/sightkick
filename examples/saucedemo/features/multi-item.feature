Feature: Multi-item cart
  As a shopper
  I want to add more than one item before checking out
  So that a single order can cover everything I need

  Background:
    Given I am logged in as "standard_user"

  Scenario: Add one item from its detail page, another from the catalog, then check out
    When I open "Sauce Labs Backpack"
    And I add it to the cart
    And I go back to the catalog
    And I add "Sauce Labs Bike Light" to the cart from the catalog
    And I go to the cart
    Then the cart contains 2 items
    When I check out with first name "Ada", last name "Lovelace", postal code "30301"
    And I place the order
    Then the order is confirmed
