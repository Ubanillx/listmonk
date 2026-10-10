/* eslint-env mocha */
/* global cy, expect */

describe('Import with empty customer codes', () => {
  beforeEach(() => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/customers/import');
  });

  it('imports private customers with empty codes and preserves existing codes on reimport', () => {
    cy.request('POST', '/api/customer-lists', { name: 'Empty codes', type: 'private', optin: 'single' });
    cy.visit('/admin/customers/import');
    cy.get('.customer_list-selector input').type('Empty codes');
    cy.contains('.customer_list-selector .autocomplete a', 'Empty codes').click();
    const upload = (fileContent) => cy.get('input[type=file]').attachFile({ fileContent, fileName: 'empty-codes.csv', mimeType: 'text/csv' });
    upload('email,name,customer_code\nblank@example.com,Blank,\nspace@example.com,Space,   \ncoded@example.com,Coded,000123\n');
    cy.contains('.help', 'The customer code column is required; its values may be blank.').should('be.visible');
    cy.get('[data-cy=btn-upload-import]').click();
    cy.get('.modal button.is-primary').click();
    cy.get('section.wrap .has-text-success', { timeout: 20000 });
    ['blank@example.com', 'space@example.com', 'coded@example.com'].forEach((email, i) => {
      cy.request(`/api/customers?search=${email}`).then(({ body }) => {
        expect(body.data.total).to.eq(1);
        expect(body.data.results[0].customer_code).to.eq(i === 2 ? '000123' : '');
      });
    });
    cy.get('button.is-primary').click();
    upload('email,name,customer_code\ncoded@example.com,Updated,\n');
    cy.get('[data-cy=btn-upload-import]').click();
    cy.get('.modal button.is-primary').click();
    cy.get('section.wrap .has-text-success', { timeout: 20000 });
    cy.request('/api/customers?search=coded@example.com').then(({ body }) => {
      expect(body.data.total).to.eq(1);
      expect(body.data.results[0]).to.include({ name: 'Updated', customer_code: '000123' });
    });
  });

  it('imports and blocklists distinct public-pool customers with empty codes without conflicts', () => {
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Empty code department', manager_user_id: body.data.id,
    }));
    let poolID;
    cy.request('POST', '/api/customer-lists', { name: 'Empty code pool', type: 'pool', optin: 'single' })
      .then(({ body }) => { poolID = body.data.id; });
    cy.visit('/admin/customers/import');
    cy.get('[data-cy=import-tab-pool]').click();
    const fileContent = 'customer_code,name,email,allocation_department\n'
      + ',One,one@example.com,Empty code department\n'
      + '   ,Two,two@example.com,Empty code department\n'
      + ',One,ONE@example.com,Empty code department\n';
    const upload = () => cy.get('input[type=file]').attachFile({ fileContent, fileName: 'empty-codes.csv', mimeType: 'text/csv' });
    upload();
    cy.contains('.help', 'The customer code column is required; its values may be blank.').should('be.visible');
    cy.get('.customer_list-selector input').type('Empty code pool');
    cy.contains('.customer_list-selector .autocomplete a', 'Empty code pool').click();
    cy.intercept('POST', '/api/import/customers').as('importPool');
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importPool').its('response.body.data').should('include', {
      created: 2, invalid: 0, duplicates: 1, conflicts: 0,
    });
    cy.get('[data-cy=check-blocklist] .check').click();
    upload();
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importPool').its('response.body.data').should('include', {
      created: 0, existing: 2, blocklisted: 2, conflicts: 0,
    });
    cy.then(() => cy.request(`/api/customer-lists/${poolID}/pool-contacts`)).then(({ body }) => {
      expect(body.data.total).to.eq(2);
      expect(body.data.results.every((row) => row.customer_code === '' && row.status === 'blocklisted')).to.eq(true);
    });
    cy.get('[data-cy=pool-result-total]').closest('.message').screenshot('import-empty-customer-codes');
  });
});
