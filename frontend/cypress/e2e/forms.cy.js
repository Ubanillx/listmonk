const apiUrl = Cypress.env('apiUrl');

describe('Forms', () => {
  it('Opens forms page', () => {
    cy.resetDB();
    cy.loginAndVisit('/admin/customer-lists/forms');
  });

  it('Checks public customer_lists', () => {
    cy.get('ul[data-cy=customer_lists] li')
      .should('contain', 'Opt-in customer_list')
      .its('length')
      .should('eq', 1);

    cy.get('[data-cy=form] [role=textbox]').should('not.exist');
  });

  it('Selects public customer_list', () => {
    // Click the customer_list checkbox.
    cy.get('ul[data-cy=customer_lists] .checkbox').click();

    // Check that the ID of the customer_list in the checkbox appears in the HTML.
    cy.get('ul[data-cy=customer_lists] input').then(($inp) => {
      cy.get('[role=textbox]').contains($inp.val());
    });

    // Click the customer_list checkbox.
    cy.get('ul[data-cy=customer_lists] .checkbox').click();
    cy.get('[data-cy=form] pre').should('not.exist');
  });

  it('Subscribes from public form page', () => {
    // Create a public test customer_list.
    cy.request('POST', `${apiUrl}/api/customer-lists`, { name: 'test-customer_list', type: 'public', optin: 'single' });

    // Open the public page and subscribe to alternating customer_lists multiple times.
    // There should be no errors and two new customers should be subscribed to two customer_lists.
    for (let i = 0; i < 2; i++) {
      for (let j = 0; j < 2; j++) {
        cy.loginAndVisit(`${apiUrl}/subscription/form`);
        cy.get('input[name=email]').clear().type(`test${i}@test.com`);
        cy.get('input[name=name]').clear().type(`test${i}`);
        cy.get('input[type=checkbox]').eq(j).click();
        cy.get('button').click();
        cy.wait(250);
        cy.get('.wrap').contains(/has been sent|successfully|retry/); // If SMTP is not configured, it shows retry message.
      }
    }

    // Verify form subscriptions.
    cy.request(`${apiUrl}/api/customers`).should((response) => {
      const { data } = response.body;

      // Two new + two dummy customers that are there by default.
      expect(data.total).to.equal(4);

      // The two new customers should each have two customer_list subscriptions.
      for (let i = 0; i < 2; i++) {
        expect(data.results.find((s) => s.email === `test${i}@test.com`).customer_lists.length).to.equal(2);
      }
    });
  });

  it('Unsubscribes', () => {
    // Add all customer_lists to the dummy campaign.
    cy.request('PUT', `${apiUrl}/api/campaigns/1`, { customer_lists: [2] });

    cy.request('GET', `${apiUrl}/api/customers`).then((response) => {
      const subUUID = response.body.data.results[0].uuid;

      cy.request('GET', `${apiUrl}/api/campaigns`).then((response) => {
        const campUUID = response.body.data.results[0].uuid;
        cy.loginAndVisit(`${apiUrl}/subscription/${campUUID}/${subUUID}`);
      });
    });

    cy.wait(500);

    // Unsubscribe from one customer_list.
    cy.get('button').click();
    cy.request('GET', `${apiUrl}/api/customers`).then((response) => {
      const { data } = response.body;
      expect(data.results[0].customer_lists.find((s) => s.id === 2).subscription_status).to.equal('unsubscribed');
      expect(data.results[0].customer_lists.find((s) => s.id === 3).subscription_status).to.equal('unconfirmed');
    });

    // Go back.
    cy.url().then((u) => {
      cy.loginAndVisit(u);
    });

    // Unsubscribe from all.
    cy.get('#privacy-blocklist').click();
    cy.get('button').click();

    cy.request('GET', `${apiUrl}/api/customers`).then((response) => {
      const { data } = response.body;
      expect(data.results[0].status).to.equal('blocklisted');
      expect(data.results[0].customer_lists.find((s) => s.id === 2).subscription_status).to.equal('unsubscribed');
      expect(data.results[0].customer_lists.find((s) => s.id === 3).subscription_status).to.equal('unsubscribed');
    });
  });

  it('Manages subscription preferences', () => {
    cy.request('GET', `${apiUrl}/api/customers`).then((response) => {
      const subUUID = response.body.data.results[1].uuid;

      cy.request('GET', `${apiUrl}/api/campaigns`).then((response) => {
        const campUUID = response.body.data.results[0].uuid;
        cy.loginAndVisit(`${apiUrl}/subscription/${campUUID}/${subUUID}?manage=1`);
        cy.get('a').contains('Manage').click();
      });
    });

    // Change name and unsubscribe from one customer_list.
    cy.get('input[name=name]').clear().type('new-name');
    cy.get('ul.customer_lists input:first').click();
    cy.get('button:first').click();

    cy.request('GET', `${apiUrl}/api/customers`).then((response) => {
      const { data } = response.body;
      expect(data.results[1].name).to.equal('new-name');
      expect(data.results[1].customer_lists.find((s) => s.id === 2).subscription_status).to.equal('unsubscribed');
      expect(data.results[1].customer_lists.find((s) => s.id === 3).subscription_status).to.equal('unconfirmed');
    });
  });
});
