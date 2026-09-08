Feature: Checking out with nothing in the cart
  As Burrito Co.
  I want an empty-cart checkout attempt to be reported, not silently accepted
  So that a $0.00 order is impossible

  Scenario: The empty-cart fixture blocks checkout
    Given I load the "empty-cart-checkout" fixture on the cart page
    Then the cart is empty
    And I see the "empty-cart-checkout" error
    When I try to check out
    Then the empty-cart message is still shown
