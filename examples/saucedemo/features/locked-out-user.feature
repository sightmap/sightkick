Feature: Login
  As saucedemo
  I want a locked-out account to be rejected with a clear reason
  So that support can tell a real lockout from a bare failure

  Scenario: A locked-out user cannot log in
    Given I am on the login page
    When I log in as "locked_out_user"
    Then I see the error "Sorry, this user has been locked out"
