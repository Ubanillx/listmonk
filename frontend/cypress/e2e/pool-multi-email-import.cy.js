/* eslint-env mocha */
/* global cy, expect */
import * as XLSX from 'xlsx';

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
      + 'C-002,   ,"bad,email@example.com;three@example.com",Source department,Multi email department,second-reply@example.com\n';
    const upload = () => cy.get('input[type=file]').attachFile({ fileContent, fileName: 'multi-email.csv', mimeType: 'text/csv' });
    upload();
    cy.get('.preview-table').should('be.visible');
    cy.contains('.help', 'The name column is required; its values may be blank.').should('be.visible');
    cy.get('[data-cy=import-map-allocation-department]').should('have.value', '分配部门');
    cy.get('.customer_list-selector input').type('Multi email pool');
    cy.contains('.customer_list-selector .autocomplete a', 'Multi email pool').click();
    cy.intercept('POST', '/api/import/customers').as('importPool');
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importPool').then(({ response }) => {
      expect(response.statusCode).to.eq(200);
      expect(response.body.data).to.include({
        total: 5, created: 3, valid: 3, invalid: 1, duplicates: 1,
      });
      expect(response.body.data.issues).to.deep.include({
        row: 3,
        customer_code: 'C-002',
        allocation_department: 'Multi email department',
        reason: 'invalid_email',
      });
    });
    cy.get('[data-cy=pool-result-total] strong').should('have.text', '5');
    cy.get('[data-cy=pool-issue-range]').should('contain', 'Rows 1-3 of 3');
    cy.get('[data-cy=pool-issue-filter]').select('invalid');
    cy.get('[data-cy=view-pool-issue]').first().click();
    cy.get('[role=dialog]').should('contain', 'Row').and('contain', 'C-002').and('contain', 'invalid email');
    cy.get('[role=dialog]').find('button').contains('Close').click();
    cy.get('[data-cy=pool-issue-filter]').select('duplicate');
    cy.get('[data-cy=pool-issue-range]').should('contain', 'Rows 1-1 of 1');
    cy.get('[data-cy=view-pool-issue]').click();
    cy.get('[role=dialog]').should('contain', 'Duplicates an earlier row');
    cy.get('[role=dialog]').find('button').contains('Close').click();
    cy.get('[data-cy=pool-issue-filter]').select('all');
    cy.get('[data-cy=export-pool-import-issues]').click();
    cy.readFile('cypress/downloads/customer-import-errors.xlsx', null).should('exist');
    cy.then(() => cy.request(`/api/customer-lists/${poolID}/pool-contacts`)).then(({ body }) => {
      expect(body.data.total).to.eq(3);
      ['one@example.com', 'two@example.com'].forEach((email) => {
        expect(body.data.results.find((row) => row.email === email)).to.include({
          customer_code: 'C-001', name: 'Contact', allocation_department: 'Multi email department', reply_to: 'replies@example.com',
        });
      });
      expect(body.data.results.find((row) => row.email === 'three@example.com')).to.include({
        customer_code: 'C-002', name: '', allocation_department: 'Multi email department', reply_to: 'second-reply@example.com',
      });
    });
    upload();
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importPool').its('response.body.data').should('include', {
      created: 0, existing: 3, invalid: 1, duplicates: 1,
    });
    cy.get('[data-cy=check-blocklist] .check').click();
    upload();
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importPool').its('response.body.data').should('include', { created: 0, existing: 3, blocklisted: 3 });
    cy.then(() => cy.request(`/api/customer-lists/${poolID}/pool-contacts`)).then(({ body }) => {
      expect(body.data.results.every((row) => row.status === 'blocklisted')).to.eq(true);
    });
    cy.get('[data-cy=pool-import-result]').scrollIntoView({ offset: { top: -80, left: 0 } });
    cy.screenshot('pool-multi-email-import-result', { capture: 'viewport', timeout: 60000 });
  });

  it('paginates every issue, searches, and exports all rows as safe Excel text', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.loginAndVisit('/admin/customers/import');
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Report department', manager_user_id: body.data.id,
    }));
    cy.request('POST', '/api/customer-lists', { name: 'Report pool', type: 'pool', optin: 'single' });
    cy.visit('/admin/customers/import');
    cy.get('[data-cy=import-tab-pool]').click();
    const rows = ['customer_code,name,email,allocation_department',
      'CODE,First,first@example.com,Report department',
      'CODE,First,first@example.com,Report department',
      'CODE,Second,second@example.com,Report department',
      ...Array.from({ length: 250 }, (_, i) => `${i === 0 ? '=1+1' : `P-${i}`},Name,invalid,Report department`)];
    cy.get('input[type=file]').attachFile({ fileContent: rows.join('\n'), fileName: 'many-errors.csv', mimeType: 'text/csv' });
    cy.get('.customer_list-selector input').type('Report pool');
    cy.contains('.customer_list-selector .autocomplete a', 'Report pool').click();
    cy.intercept('POST', '/api/import/customers').as('importReport');
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importReport').then(({ response }) => {
      expect(response.body.data).to.include({
        total: 253, created: 2, conflicts: 1, invalid: 250, duplicates: 1,
      });
      expect(response.body.data.issues).to.have.length(252);
      response.body.data.issues.forEach((issue) => expect(issue).to.have.all.keys('row', 'customer_code', 'allocation_department', 'reason'));
    });
    cy.get('.import-issues tbody tr').should('have.length', 20);
    cy.get('[data-cy=pool-issue-range]').should('contain', '第 1-20 条，共 252 条');
    cy.get('.import-issues [data-cy=page-jump]').clear().type('13');
    cy.get('.import-issues [data-cy=page-jump-submit]').click();
    cy.get('[data-cy=pool-issue-range]').should('contain', '第 241-252 条，共 252 条');
    cy.get('.import-issues tbody tr').should('have.length', 12);
    cy.get('.import-issues [data-cy=page-size]').select('100');
    cy.get('[data-cy=pool-issue-range]').should('contain', '第 1-100 条，共 252 条');
    cy.get('[data-cy=pool-issue-filter]').select('invalid');
    cy.get('[data-cy=pool-issue-range]').should('contain', '第 1-100 条，共 250 条');
    cy.get('[data-cy=pool-issue-search]').type('P-249');
    cy.get('[data-cy=pool-issue-range]').should('contain', '第 1-1 条，共 1 条');
    cy.get('[data-cy=view-pool-issue]').click();
    cy.get('[role=dialog]').should('contain', 'P-249').and('contain', '254').and('contain', 'Report department');
    cy.get('[role=dialog]').find('button').contains('关闭').click();
    // Export is complete even when the screen shows only one filtered row.
    cy.get('[data-cy=export-pool-import-issues]').click();
    // Retry the read until this download replaces the preceding test's report.
    cy.readFile('cypress/downloads/customer-import-errors.xlsx', null).should((buffer) => {
      const workbook = XLSX.read(buffer, { type: 'buffer' });
      const sheet = workbook.Sheets[workbook.SheetNames[0]];
      const data = XLSX.utils.sheet_to_json(sheet, { header: 1 });
      expect(data).to.have.length(253);
      expect(data[0]).to.deep.equal(['行号', '客户编号', '分配部门', '错误类型', '错误原因']);
      expect(data[252][1]).to.eq('P-249');
      expect(sheet.B4).to.include({ t: 's', v: '=1+1' });
      expect(sheet.B4).not.to.have.property('f');
      expect(JSON.stringify(data)).not.to.contain('example.com');
    });
    cy.get('[data-cy=pool-issue-search]').clear().type('no-matching-row');
    cy.get('.import-issues-empty').should('be.visible');
    cy.get('[data-cy=pool-issue-range]').should('contain', '第 0-0 条，共 0 条');
    cy.get('[data-cy=pool-issue-search]').clear();
    cy.get('.metric-warning').click();
    cy.get('[data-cy=pool-issue-filter]').should('have.value', 'conflict');
    cy.get('.import-issues tbody tr').should('have.length', 1).and('contain', '已导入为新联系人');
    cy.get('[data-cy=pool-issue-filter]').select('all');
    cy.get('.import-issues [data-cy=page-size]').select('10');
    cy.viewport(1440, 1000);
    cy.get('[data-cy=pool-import-result]').should(($result) => {
      const bounds = $result[0].getBoundingClientRect();
      expect(bounds.right).to.be.at.most(1440);
    });
    cy.viewport(1280, 720);
    cy.document().then((doc) => expect(doc.documentElement.scrollWidth).to.be.at.most(1280));
    cy.get('[data-cy=pool-import-result]').scrollIntoView({ offset: { top: -80, left: 0 } });
    cy.screenshot('pool-import-errors-desktop', { capture: 'viewport' });
    cy.get('.navbar').invoke('css', 'visibility', 'hidden');
    cy.get('[data-cy=pool-import-result]').screenshot('pool-import-errors-desktop-panel', { timeout: 60000 });
    cy.get('.navbar').invoke('css', 'visibility', '');
    cy.viewport(390, 844);
    cy.get('[data-cy=pool-import-result]').scrollIntoView({ offset: { top: -64, left: 0 } });
    cy.screenshot('pool-import-errors-mobile', { capture: 'viewport' });
    cy.get('.navbar').invoke('css', 'visibility', 'hidden');
    cy.get('[data-cy=pool-import-result]').screenshot('pool-import-errors-mobile-panel', { timeout: 60000 });
    cy.get('.navbar').invoke('css', 'visibility', '');
    cy.document().then((doc) => expect(doc.documentElement.scrollWidth).to.be.at.most(390));
    cy.viewport(1400, 950);
    cy.get('input[type=file]').attachFile({
      fileContent: `${rows[0]}\n${rows[1]}`, fileName: 'valid.csv', mimeType: 'text/csv',
    });
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importReport').its('response.body.data').should('include', { existing: 1, invalid: 0, duplicates: 0 });
    cy.get('.import-issues-empty').should('contain', '本次导入没有异常记录');
    cy.get('[data-cy=export-pool-import-issues]').should('be.disabled');
  });
});
