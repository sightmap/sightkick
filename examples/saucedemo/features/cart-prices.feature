Feature: Cart prices
  As a shopper
  I want to see the price of each item in my cart
  So that I can confirm what I'm about to pay before checkout

  Background:
    Given I am logged in as "standard_user"

  Scenario: Cart line items show name and price together
    When I add "Sauce Labs Backpack" to the cart from the catalog
    And I add "Sauce Labs Bike Light" to the cart from the catalog
    And I go to the cart
    Then the cart shows "Sauce Labs Backpack" at "$29.99"
    And the cart shows "Sauce Labs Bike Light" at "$9.99"
