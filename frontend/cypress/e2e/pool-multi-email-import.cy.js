/* eslint-env mocha */
/* global cy, expect */

describe('Public pool multi-email import', () => {
  it('expands email cells, reuses row fields and reports invalid source rows', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/customers/import');
    let poolID;
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Multi email department', manager_user_id: body.data.id,
    }));
    cy.request('POST', '/api/customer-lists', { name: 'Multi email pool', type: 'pool', optin: 'single' })
      .then(({ body }) => { poolID = body.data.id; });
    cy.visit('/admin/customers/import');
    cy.get('[data-cy=import-tab-pool]').click();
    const fileContent = '客户编号,姓名,邮箱,部门,分配部门,回信邮箱\n'
      + 'C-001,Contact,"one@example.com;two@example.com\r\none@example.com",Source department,Multi email department,replies@example.com\n'
      + 'C-002,Second,"bad,email@example.com;three@example.com",Source department,Multi email department,second-reply@example.com\n';
    const upload = () => cy.get('input[type=file]').attachFile({ fileContent, fileName: 'multi-email.csv', mimeType: 'text/csv' });
    upload();
    cy.get('.preview-table').should('be.visible');
    cy.get('[data-cy=import-map-allocation-department]').should('have.value', '分配部门');
    cy.get('.customer_list-selector input').type('Multi email pool');
    cy.contains('.customer_list-selector .autocomplete a', 'Multi email pool').click();
    cy.intercept('POST', '/api/import/customers').as('importPool');
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importPool').then(({ response }) => {
      expect(response.statusCode).to.eq(200);
      expect(response.body.data).to.include({ total: 5, created: 3, valid: 3, invalid: 1, duplicates: 1 });
      expect(response.body.data.issues[0]).to.include({ row: 3, customer_code: 'C-002', reason: 'invalid_email' });
    });
    cy.get('[data-cy=pool-result-total]').should('contain', '5 contact records');
    cy.then(() => cy.request(`/api/customer-lists/${poolID}/pool-contacts`)).then(({ body }) => {
      expect(body.data.total).to.eq(3);
      ['one@example.com', 'two@example.com'].forEach((email) => {
        expect(body.data.results.find((row) => row.email === email)).to.include({
          customer_code: 'C-001', name: 'Contact', allocation_department: 'Multi email department', reply_to: 'replies@example.com',
        });
      });
      expect(body.data.results.find((row) => row.email === 'three@example.com')).to.include({
        customer_code: 'C-002', name: 'Second', allocation_department: 'Multi email department', reply_to: 'second-reply@example.com',
      });
    });
    upload();
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importPool').its('response.body.data').should('include', { created: 0, existing: 3, invalid: 1, duplicates: 1 });
    cy.get('[data-cy=check-blocklist] .check').click();
    upload();
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importPool').its('response.body.data').should('include', { created: 0, existing: 3, blocklisted: 3 });
    cy.then(() => cy.request(`/api/customer-lists/${poolID}/pool-contacts`)).then(({ body }) => {
      expect(body.data.results.every((row) => row.status === 'blocklisted')).to.eq(true);
    });
    cy.get('[data-cy=pool-result-total]').closest('.message').screenshot('pool-multi-email-import-result');
  });
});
