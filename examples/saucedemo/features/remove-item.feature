Feature: Remove from cart
  As a shopper
  I want to remove an item I changed my mind about
  So that I only pay for what I actually want

  Background:
    Given I am logged in as "standard_user"

  Scenario: Remove an item before checking out
    When I add "Sauce Labs Backpack" to the cart from the catalog
    And I go to the cart
    And I remove "Sauce Labs Backpack" from the cart
    Then the cart is empty
