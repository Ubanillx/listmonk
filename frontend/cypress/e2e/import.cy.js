describe('Import', () => {
  it('Opens import page', () => {
    cy.resetDB();
    cy.loginAndVisit('/admin/customers/import');
  });

  it('Imports customers', () => {
    cy.request('POST', '/api/customer-lists', { name: 'Import single opt-in', type: 'private', optin: 'single' })
      .then(({ body }) => body.data.id).then((singleID) => {
        cy.request('POST', '/api/customer-lists', { name: 'Import double opt-in', type: 'private', optin: 'double' })
          .then(({ body }) => {
            const doubleID = body.data.id;
            cy.get('[data-cy=import-subscription-status]').should('not.exist');
            cy.get('[data-cy=overwrite-user-info], [data-cy=overwrite-sub-status]').should('not.exist');
            cy.get('.customer_list-selector input').type('Import single opt-in');
            cy.contains('.customer_list-selector .autocomplete a', 'Import single opt-in').click();
            cy.get('[data-cy=import-subscription-status]').should('not.exist');
            cy.get('.customer_list-selector input').type('Import double opt-in');
            cy.contains('.customer_list-selector .autocomplete a', 'Import double opt-in').click();
            cy.get('input[name=subStatus][value=confirmed]').should('be.checked');
            cy.get('[data-cy=check-unconfirmed] .check').click();
            cy.contains('.customer_list-selector .customer_list', 'Import double opt-in').find('.delete').click();
            cy.get('[data-cy=import-subscription-status]').should('not.exist');
            cy.get('.customer_list-selector input').type('Import double opt-in');
            cy.contains('.customer_list-selector .autocomplete a', 'Import double opt-in').click();
            cy.get('input[name=subStatus][value=confirmed]').should('be.checked');

            const cases = [
              { mode: 'subscribe', listID: singleID, double: false, subStatus: 'confirmed' },
              { mode: 'subscribe', listID: doubleID, double: true, subStatus: 'unconfirmed' },
              { mode: 'subscribe', listID: doubleID, double: true, subStatus: 'confirmed' },
              { mode: 'subscribe', listID: singleID, double: false, subStatus: 'confirmed' },
              { mode: 'blocklist', listID: singleID, double: false, subStatus: 'unsubscribed' },
            ];
            cases.forEach((c, index) => {
              cy.loginAndVisit('/admin/customers/import');
              const listName = c.listID === doubleID ? 'Import double opt-in' : 'Import single opt-in';
              cy.get('.customer_list-selector input').type(listName);
              cy.contains('.customer_list-selector .autocomplete a', listName).click();
              cy.get('.customer_list-selector .customer_list').should('exist');
              cy.get(`[data-cy=check-${c.mode}] .check`).click();
              if (c.double) {
                cy.get('input[name=subStatus][value=confirmed]').should('be.checked');
                cy.get(`[data-cy=check-${c.subStatus}] .check`).click();
              } else {
                cy.get('[data-cy=import-subscription-status]').should('not.exist');
              }
              const name = `Updated import ${index}`;
              cy.fixture('subs.csv').then((data) => {
                cy.get('input[type="file"]').attachFile({
                  fileContent: data.toString().replace('First0 Last0', name),
                  fileName: 'subs.csv',
                  mimeType: 'text/csv',
                });
              });
              cy.get('.preview-table', { timeout: 10000 });
              cy.get('button.is-primary').click();
              if (c.mode === 'subscribe') {
                cy.get('.modal button.is-primary').click();
              }
              cy.get('section.wrap .has-text-success', { timeout: 15000 });
              cy.request(`/api/customers?search=user0@mail.com&customer_list_id=${c.listID}`)
                .then(({ body: result }) => {
                  expect(result.data.results[0].name).to.equal(c.mode === 'subscribe' ? name : 'Updated import 3');
                  expect(result.data.results[0].status).to.equal(c.mode === 'subscribe' ? 'enabled' : 'blocklisted');
                });
              cy.request(`/api/customers?customer_list_id=${c.listID}&subscription_status=${c.subStatus}`)
                .its('body.data.total').should('eq', 100);
              cy.get('button.is-primary').click();
            });
          });
    });
  });

  it('Imports customers incorrectly', () => {
    cy.wait(1000);
    cy.resetDB();
    cy.wait(1000);
    cy.loginAndVisit('/admin/customers/import');

    // Blocklist mode permits uploading a file without a customer-code mapping.
    cy.get('[data-cy=check-blocklist] .check').click();
    cy.get('input[name=delim]').should('not.exist');

    cy.fixture('subs.csv').then((data) => {
      cy.get('input[type="file"]').attachFile({
        fileContent: data.toString().replace(/^email,name,customer_code.*\r?\n/, 'invalid_header\n'),
        fileName: 'subs.csv',
        mimeType: 'text/csv',
      });
    });

    cy.get('button.is-primary').click();
    cy.wait(250);
    cy.get('section.wrap .has-text-danger');
    cy.get('button.is-primary').click();
    cy.get('[data-cy=check-subscribe]').should('be.visible');
  });

  it('creates a private list and selects it on the import page', () => {
    const name = `import-flow-${Date.now()}`;
    cy.loginAndVisit('/admin/customers/import');
    cy.get('[data-cy=import-tab-private]').should('have.attr', 'aria-selected', 'true');
    cy.get('[data-cy=import-create-list]').click();
    cy.get('.modal input[name=name]').clear().type(name);
    cy.get('.modal [data-cy=btn-save]').click();
    cy.get('.customer_list-selector .customer_list').should('contain', name);
    cy.url().should('include', '/admin/customers/import');

    cy.request('/api/customer-lists?minimal=true&per_page=all&status=active&type_group=private')
      .then(({ body }) => {
        const list = body.data.results.find((item) => item.name === name);
        expect(list, 'created import list').to.exist;
        cy.request('DELETE', `/api/customer-lists/${list.id}`);
      });
  });
});
