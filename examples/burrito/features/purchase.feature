Feature: Order a burrito
  As a hungry customer
  I want to customize an item and pay for it
  So that I get a confirmed order

  Scenario: Order two steak burritos with a promo code
    Given the menu lists five items
    When I open "Classic Burrito"
    And I customize "protein" as "steak"
    And I increase the quantity to 2
    And I add it to the cart
    Then the cart holds one line for "Classic Burrito" at "$21.90"
    When I check out
    And I enter the delivery address "123 Main St", "Denver", "CO", "80203"
    And I pay with card "4242 4242 4242 4242" expiring "09/26"
    And I apply the promo code "BURRITO20"
    Then the order total is "$18.92"
    When I place the order
    Then I get an order id
