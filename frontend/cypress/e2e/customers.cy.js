const apiUrl = Cypress.env('apiUrl');

describe('Customers', () => {
  it('Opens customers page', () => {
    cy.resetDB();
    cy.loginAndVisit('/admin/customers');
  });

  it('Counts customers', () => {
    cy.get('tbody td[data-label=E-mail]').its('length').should('eq', 2);
    cy.get('thead th').eq(1).should('have.class', 'cy-customer_code');
    cy.get('tbody td[data-label=CustomerLists] a').first()
      .should('have.attr', 'href').and('match', /\/customers\/customer_lists\/\d+$/);
  });

  it('Searches customers', () => {
    const cases = [
      { value: 'john{enter}', count: 1, contains: 'john@example.com' },
      { value: 'anon{enter}', count: 1, contains: 'anon@example.com' },
      { value: '{enter}', count: 2, contains: null },
    ];

    cases.forEach((c) => {
      cy.get('[data-cy=search]').clear().type(c.value);
      cy.get('tbody td[data-label=E-mail]').its('length').should('eq', c.count);
      if (c.contains) {
        cy.get('tbody td[data-label=E-mail]').contains(c.contains);
      }
    });
  });

  it('Exports customers', () => {
    const cases = [
      {
        customerCustomerListIDs: [], ids: [], query: '', length: 3,
      },
      {
        customerCustomerListIDs: [], ids: [], query: "name ILIKE '%anon%'", length: 2,
      },
      {
        customerCustomerListIDs: [], ids: [], query: "name like 'nope'", length: 1,
      },
    ];

    // customerCustomerListIDs[] and ids[] are unused for now as Cypress doesn't support encoding of arrays in `qs`.
    cases.forEach((c) => {
      cy.request({ url: `${apiUrl}/api/customers/export`, qs: { query: c.query, customer_list_id: c.customerCustomerListIDs, id: c.ids } }).then((resp) => {
        cy.expect(resp.body.trim().split('\n')).to.have.lengthOf(c.length);
      });
    });
  });

  it('Advanced searches customers', () => {
    cy.get('[data-cy=btn-advanced-search]').click();

    const cases = [
      { value: 'customers.attribs->>\'city\'=\'Bengaluru\'', count: 2 },
      { value: 'customers.attribs->>\'city\'=\'Bengaluru\' AND id=1', count: 1 },
      { value: '(customers.attribs->>\'good\')::BOOLEAN = true AND name like \'Anon%\'', count: 1 },
    ];

    cases.forEach((c) => {
      cy.get('[data-cy=query]').clear().type(c.value);
      cy.get('[data-cy=btn-query]').click();
      cy.get('tbody td[data-label=E-mail]').its('length').should('eq', c.count);
    });

    cy.get('[data-cy=btn-query-reset]').click();
    cy.wait(1000);
    cy.get('tbody td[data-label=E-mail]').its('length').should('eq', 2);
  });

  it('Does bulk customer customer_list add and remove', () => {
    const cases = [
      // radio: action to perform, rows: table rows to select and perform on: [expected statuses of those rows after thea action]
      { radio: 'check-customer_list-add', customer_lists: [0, 1], rows: { 0: ['confirmed', 'confirmed'] } },
      { radio: 'check-customer_list-unsubscribe', customer_lists: [0, 1], rows: { 0: ['unsubscribed', 'unsubscribed'], 1: ['unsubscribed'] } },
      { radio: 'check-customer_list-remove', customer_lists: [0, 1], rows: { 1: [] } },
      { radio: 'check-customer_list-add', customer_lists: [0, 1], rows: { 0: ['unsubscribed', 'unsubscribed'], 1: ['unconfirmed', 'unconfirmed'] } },
      { radio: 'check-customer_list-remove', customer_lists: [0], rows: { 0: ['unsubscribed'] } },
      { radio: 'check-customer_list-add', customer_lists: [0], rows: { 0: ['unconfirmed', 'unsubscribed'] } },
    ];

    cases.forEach((c, n) => {
      // Select one of the 2 customers in the table.
      Object.keys(c.rows).forEach((r) => {
        cy.get('tbody td.checkbox-cell .checkbox').eq(r).click();
      });

      // Open the 'manage customer_lists' modal.
      cy.get('[data-cy=btn-manage-customer_lists]').click();

      // Check both customer_lists in the modal.
      c.customer_lists.forEach((l) => {
        cy.get('.customer_list-selector input').click();
        cy.get('.customer_list-selector .autocomplete a').first().click();
      });

      // Select the radio option in the modal.
      cy.get(`[data-cy=${c.radio}] .check`).click();

      // For the first test, check the optin preconfirm box.
      if (n === 0) {
        cy.get('[data-cy=preconfirm]').click();
      }

      // Save.
      cy.get('.modal button.is-primary').click();

      // Check that each customer_list is displayed as a link in the CustomerLists column.
      Object.keys(c.rows).forEach((r) => {
        cy.get('tbody td[data-label=CustomerLists]').eq(r).find('a').should('have.length', c.rows[r].length);
      });
    });
  });

  it('Resets customers page', () => {
    cy.resetDB();
    cy.loginAndVisit('/admin/customers');
  });

  it('Edits customers', () => {
    const status = ['enabled', 'blocklisted'];
    const json = '{"string": "hello", "ints": [1,2,3], "null": null, "sub": {"bool": true}}';

    // Collect values being edited on each sub to confirm the changes in the next step
    // index by their ID shown in the modal.
    const rows = {};

    // Open the edit popup and edit the default customer_lists.
    cy.get('[data-cy=btn-edit]').each(($el, n) => {
      const email = `email-${n}@EMAIL.com`;
      const name = `name-${n}`;

      // Open the edit modal.
      cy.wrap($el).click();

      // Get the ID from the header and proceed to fill the form.
      let id = 0;
      cy.get('[data-cy=id]').then(($el) => {
        id = parseInt($el.text());

        cy.get('input[name=email]').clear().type(email);
        cy.get('input[name=customer_code]').clear().type(`CUST-EDIT-${n}`);
        cy.get('input[name=name]').clear().type(name);

        if (status[n] === 'blocklisted') {
          cy.get('select[name=status]').select(status[n]);
        }
        cy.get('.customer_list-selector input').click();
        cy.get('.customer_list-selector .autocomplete a').first().click();
        cy.get('textarea[name=attribs]').clear().type(json, { parseSpecialCharSequences: false, delay: 0 });
        cy.get('.modal-card-foot button[type=submit]').click();

        rows[id] = { email, name, status: status[n] };
      });
    });

    // Confirm the edits on the table.
    cy.wait(500);
    cy.log(rows);
    cy.get('tbody tr').each(($el) => {
      cy.wrap($el).find('td[data-id]').invoke('attr', 'data-id').then((idStr) => {
        const id = parseInt(idStr);
        cy.wrap($el).find('td[data-label=E-mail]').contains(rows[id].email.toLowerCase());
        cy.wrap($el).find('td[data-label=Salutation]').contains(rows[id].name);

        if (rows[id].status === 'blocklisted') {
          cy.wrap($el).find('[data-cy=blocklisted]');
        }

        cy.wrap($el).find('td[data-label=CustomerLists] a').its('length').should('eq', 2);
      });
    });
  });

  it('Deletes customers', () => {
    // Delete all visible customer_lists.
    cy.get('tbody tr').each(() => {
      cy.get('tbody a[data-cy=btn-delete]').first().click();
      cy.get('.modal button.is-primary').click();
    });

    // Confirm deletion.
    cy.get('table tr.is-empty');
  });

  it('Creates new customers', () => {
    const statuses = ['enabled', 'blocklisted'];
    const customer_lists = [[1], [2], [1, 2]];
    const json = '{"string": "hello", "ints": [1,2,3], "null": null, "sub": {"bool": true}}';

    // Cycle through each status and each customer_list ID combination and create customers.
    const n = 0;
    for (let n = 0; n < 6; n++) {
      const email = `email-${n}@EMAIL.com`;
      const name = `name-${n}`;
      const status = statuses[(n + 1) % statuses.length];
      const customer_list = customer_lists[(n + 1) % customer_lists.length];

      cy.get('[data-cy=btn-new]').click();
      cy.get('.modal-card-body input').then(($inputs) => {
        const names = [...$inputs].map((input) => input.name);
        expect(names.indexOf('customer_code')).to.be.lessThan(names.indexOf('email'));
      });
      cy.get('input[name=email]').type(email);
      cy.get('input[name=customer_code]').type(`CUST-${n}`);
      cy.get('input[name=name]').type(name);
      cy.get('select[name=status]').select(status);

      customer_list.forEach((l) => {
        cy.get('.customer_list-selector input').click();
        cy.get('.customer_list-selector .autocomplete a').first().click();
      });
      cy.get('textarea[name=attribs]').clear().type(json, { parseSpecialCharSequences: false, delay: 0 });
      cy.get('.modal-card-foot button[type=submit]').click();

      // Confirm the addition by inspecting the newly created customer_list row,
      // which is always the first row in the table.
      cy.wait(250);
      const tr = cy.get('tbody tr:nth-child(1)').then(($el) => {
        cy.wrap($el).find('td[data-label=E-mail]').contains(email.toLowerCase());
        cy.wrap($el).find('td[data-label=Salutation]').contains(name);

        if (status === 'blocklisted') {
          cy.wrap($el).find('[data-cy=blocklisted]');
        }
        cy.wrap($el).find('td[data-label=CustomerLists] a').its('length').should('eq', customer_list.length);
      });
    }
  });

  it('Sorts customers', () => {
    const asc = [3, 4, 5, 6, 7, 8];
    const desc = [8, 7, 6, 5, 4, 3];
    const cases = ['cy-email', 'cy-name', 'cy-created_at', 'cy-updated_at'];

    cases.forEach((c) => {
      cy.sortTable(`thead th.${c}`, asc);
      cy.wait(250);
      cy.sortTable(`thead th.${c}`, desc);
      cy.wait(250);
    });
  });
});

describe('Domain blocklist', () => {
  it('Opens settings page', () => {
    cy.resetDB();
  });

  it('Add domains to blocklist', () => {
    cy.loginAndVisit('/admin/settings');
    cy.get('.b-tabs nav a').eq(2).click();
    cy.get('textarea[name="privacy.domain_blocklist"]').clear().type('ban.net\n\nBaN.OrG\n\nban.com\n\n');
    cy.get('[data-cy=btn-save]').click();
  });

  it('Try subscribing via public page', () => {
    cy.wait(1000);
    cy.visit(`${apiUrl}/subscription/form`);
    cy.get('input[name=email]').clear().type('test@noban.net');
    cy.get('button[type=submit]').click();
    cy.get('h2').contains('Subscribe');

    cy.visit(`${apiUrl}/subscription/form`);
    cy.get('input[name=email]').clear().type('test@ban.net');
    cy.get('button[type=submit]').click();
    cy.get('h2').contains('Error');
  });

  // Post to the admin API.
  it('Try via admin API', () => {
    cy.wait(1000);

    // Add non-banned domain.
    cy.request({
      method: 'POST',
      url: `${apiUrl}/api/customers`,
      failOnStatusCode: true,
      body: {
        email: 'test1@noban.net', name: 'test', customer_lists: [1], status: 'enabled', customer_code: 'CUST-NB1',
      },
    }).should((response) => {
      expect(response.status).to.equal(200);
    });

    // Add banned domain.
    cy.request({
      method: 'POST',
      url: `${apiUrl}/api/customers`,
      failOnStatusCode: false,
      body: {
        email: 'test1@ban.com', name: 'test', customer_lists: [1], status: 'enabled', customer_code: 'CUST-B1',
      },
    }).should((response) => {
      expect(response.status).to.equal(400);
    });

    // Modify an existinb customer to a banned domain.
    cy.request({
      method: 'PUT',
      url: `${apiUrl}/api/customers/1`,
      failOnStatusCode: false,
      body: {
        email: 'test3@ban.org', name: 'test', customer_lists: [1], status: 'enabled', customer_code: 'CUST-B3',
      },
    }).should((response) => {
      expect(response.status).to.equal(400);
    });
  });

  it('Try via import', () => {
    cy.loginAndVisit('/admin/customers/import');
    cy.get('.customer_list-selector input').click();
    cy.get('.customer_list-selector .autocomplete a').first().click();

    cy.fixture('subs-domain-blocklist.csv').then((data) => {
      cy.get('input[type="file"]').attachFile({
        fileContent: data.toString(),
        fileName: 'subs.csv',
        mimeType: 'text/csv',
      });
    });

    // Wait for the preview (and its automatic field mapping) to render.
    cy.get('.preview-table', { timeout: 10000 });

    cy.get('button.is-primary').click();
    cy.get('section.wrap .has-text-success');
    // cy.get('button.is-primary').click();
    cy.get('.log-view').should('contain', 'ban1-import@BAN.net').and('contain', 'ban2-import@ban.ORG');
    cy.wait(100);
  });

  it('Clear blocklist and try', () => {
    cy.loginAndVisit('/admin/settings');
    cy.get('.b-tabs nav a').eq(2).click();
    cy.get('textarea[name="privacy.domain_blocklist"]').clear();
    cy.get('[data-cy=btn-save]').click();
    cy.wait(3000);

    // Add banned domain.
    cy.request({
      method: 'POST',
      url: `${apiUrl}/api/customers`,
      failOnStatusCode: true,
      body: {
        email: 'test4@BAN.com', name: 'test', customer_lists: [1], status: 'enabled', customer_code: 'CUST-NB4',
      },
    }).should((response) => {
      expect(response.status).to.equal(200);
    });

    // Modify an existinb customer to a banned domain.
    cy.request({
      method: 'PUT',
      url: `${apiUrl}/api/customers/1`,
      failOnStatusCode: true,
      body: {
        email: 'test4@BAN.org', name: 'test', customer_lists: [1], status: 'enabled', customer_code: 'CUST-NB4',
      },
    }).should((response) => {
      expect(response.status).to.equal(200);
    });
  });
});
