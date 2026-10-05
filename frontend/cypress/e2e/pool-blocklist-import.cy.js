/* eslint-env mocha */
/* global cy, expect */

describe('Public pool blocklist import', () => {
  it('imports blocklisted contacts, matches existing emails and preserves blocking on normal import', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/customers/import');
    cy.get('[data-cy=import-tab-pool]').should('be.visible');
    let poolID;
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Blocklist import department', manager_user_id: body.data.id,
    }));
    cy.request('POST', '/api/customer-lists', { name: 'Blocklist import pool', type: 'pool', optin: 'single' })
      .then(({ body }) => {
        poolID = body.data.id;
        ['blocked', 'untouched'].forEach((name) => {
          cy.request('POST', `/api/customer-lists/${poolID}/pool-contacts`, {
            customer_code: name, name, email: `${name}@example.com`, allocation_department: 'Blocklist import department',
          });
        });
      });
    cy.visit('/admin/customers/import');
    cy.get('[data-cy=import-tab-pool]').click();
    cy.get('[data-cy=check-blocklist] .check').click();
    cy.get('[data-cy=pool-blocklist-help]').should('contain', 'Normal imports do not clear the blocklist');
    cy.get('[data-cy=import-subscription-status]').should('not.exist');
    const fileContent = 'customer_code,name,email,allocation_department\n'
      + 'changed,Changed identity,BLOCKED@example.com,Blocklist import department\n'
      + 'new,New contact,new@example.com,Blocklist import department\n';
    cy.get('input[type=file]').attachFile({ fileContent, fileName: 'pool-blocklist.csv', mimeType: 'text/csv' });
    cy.get('.preview-table').should('be.visible');
    cy.get('[data-cy=btn-upload-import]').should('be.disabled');
    cy.get('.customer_list-selector input').type('Blocklist import pool');
    cy.contains('.customer_list-selector .autocomplete a', 'Blocklist import pool').click();
    cy.intercept('POST', '/api/import/customers').as('importPool');
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importPool').then(({ response }) => {
      expect(response.statusCode).to.eq(200);
      expect(response.body.data.created).to.eq(2);
      expect(response.body.data.blocklisted).to.eq(3);
    });
    cy.get('[data-cy=pool-result-blocklisted]').should('contain', '3');
    cy.then(() => cy.request(`/api/customer-lists/${poolID}/pool-contacts`)).then(({ body }) => {
      expect(body.data.results.filter((row) => row.status === 'blocklisted')).to.have.length(3);
      expect(body.data.results.find((row) => row.customer_code === 'untouched').status).to.eq('active');
    });
    cy.get('[data-cy=check-subscribe] .check').click();
    cy.get('input[type=file]').attachFile({ fileContent, fileName: 'pool-normal.csv', mimeType: 'text/csv' });
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importPool').its('response.statusCode').should('eq', 200);
    cy.then(() => cy.request(`/api/customer-lists/${poolID}/pool-contacts`)).then(({ body }) => {
      expect(body.data.results.filter((row) => row.status === 'blocklisted')).to.have.length(3);
    });
    cy.then(() => cy.visit(`/admin/pool-lists/${poolID}/contacts`));
    cy.contains('[data-cy=pool-contacts-table] tbody tr', 'blocked@example.com').should('contain', 'Blocklisted');
  });
});
