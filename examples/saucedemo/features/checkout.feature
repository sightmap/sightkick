Feature: Checkout
  As a shopper
  I want to buy an item
  So that I receive an order confirmation

  Background:
    Given I am logged in as "standard_user"

  Scenario: Buy a single item with valid details
    When I open "Sauce Labs Backpack"
    And I add it to the cart
    And I go to the cart
    And I check out with first name "Ada", last name "Lovelace", postal code "30301"
    And I place the order
    Then the order is confirmed
