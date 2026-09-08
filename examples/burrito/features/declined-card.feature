Feature: A declined card
  As Burrito Co.
  I want a declined card reported at the point of payment
  So that no order is created

  Scenario: A card ending 0000 is declined at Place Order
    Given I have a "Classic Burrito" in the cart at checkout
    When I enter the delivery address "123 Main St", "Denver", "CO", "80203"
    And I pay with card "4242 4242 4242 0000" expiring "09/26"
    Then I reach the review step
    When I place the order
    Then the payment is declined
    And I remain on the review step, not a confirmed order
