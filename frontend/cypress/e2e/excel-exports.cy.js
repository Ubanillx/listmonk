/* eslint-env mocha */
/* global cy, expect, Cypress */
import * as XLSX from 'xlsx';

const contentType = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';

describe('Excel exports and translated import templates', () => {
  let customerID;
  let customerUUID;
  let template;
  const stamp = Date.now();

  before(() => {
    // Use the dedicated disposable export-review stack. Never reset the
    // development database or another concurrent Cypress runner.
    expect(Cypress.config('baseUrl')).to.eq('http://127.0.0.1:9473');
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/customers');
    cy.get('[data-cy=btn-export-customers]').should('be.visible');
    cy.request('POST', '/api/customers', {
      customer_code: '00001234567890123456',
      name: '=SUM(1,1)',
      email: `excel-${stamp}@example.invalid`,
      status: 'enabled',
      attribs: { company: 'Example company', notes: '第一行\n第二行' },
      customer_list_ids: [],
    }).then(({ body }) => { customerID = body.data.id; customerUUID = body.data.uuid; });
  });

  beforeEach(() => {
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
  });

  it('downloads real localized customer and privacy workbooks', () => {
    ['en', 'zh-CN', 'zh-TW'].forEach((lang) => {
      cy.then(() => cy.request({ url: `/api/customers/export?id=${customerID}&lang=${lang}`, encoding: 'binary' }))
        .then(({ body, headers }) => {
          expect(headers['content-type']).to.eq(contentType);
          expect(headers['content-disposition']).to.contain('customers.xlsx');
          const workbook = XLSX.read(body, { type: 'binary' });
          const rows = XLSX.utils.sheet_to_json(workbook.Sheets[workbook.SheetNames[0]], { header: 1 });
          expect(rows).to.have.length(2);
          expect(rows[1][0]).to.eq('00001234567890123456');
          expect(rows[1][1]).to.eq('=SUM(1,1)');
          expect(workbook.Sheets[workbook.SheetNames[0]].B2.f).to.eq(undefined);
          expect(rows[0][0]).to.eq({ en: 'Customer code', 'zh-CN': '客户编码', 'zh-TW': '客戶編碼' }[lang]);
          if (lang === 'zh-TW') expect(rows[0][3]).to.eq('客戶狀態');
          if (lang === 'zh-CN') cy.writeFile('../.codex-artifacts/excel-export-review/customers-zh-CN.xlsx', body, 'binary');
        });
    });
    [{ search: `excel-${stamp}@example.invalid`, count: 2 }, { search: `missing-${stamp}`, count: 1 }].forEach(({ search, count }) => {
      cy.request({ url: '/api/customers/export', qs: { search, lang: 'en' }, encoding: 'binary' }).then(({ body }) => {
        const workbook = XLSX.read(body, { type: 'binary' });
        const rows = XLSX.utils.sheet_to_json(workbook.Sheets[workbook.SheetNames[0]], { header: 1 });
        expect(rows).to.have.length(count);
        if (count > 1) expect(rows[1][0]).to.eq('00001234567890123456');
      });
    });
    cy.then(() => cy.request({ url: `/api/customers/${customerID}/export?lang=zh-CN`, encoding: 'binary' }))
      .then(({ body }) => {
        const workbook = XLSX.read(body, { type: 'binary' });
        expect(workbook.SheetNames).to.deep.eq(['客户资料', '列表订阅', '邮件打开', '链接点击']);
        cy.writeFile('../.codex-artifacts/excel-export-review/customer-data-zh-CN.xlsx', body, 'binary');
      });
  });

  it('exports audit records in the UI language with an xlsx filename', () => {
    cy.intercept('GET', '/api/audit-events/export*').as('auditDownload');
    cy.visit('/admin/settings/audit');
    cy.get('[data-cy=audit-export-all]').should('not.be.disabled').click();
    cy.wait('@auditDownload').then(({ request, response }) => {
      expect(request.query.lang).to.eq('en');
      expect(response.headers['content-type']).to.eq(contentType);
    });
    const filename = `audit-events-all-${new Date().toISOString().slice(0, 10)}.xlsx`;
    cy.readFile(`cypress/downloads/${filename}`, 'binary').then((body) => {
      const workbook = XLSX.read(body, { type: 'binary' });
      const rows = XLSX.utils.sheet_to_json(workbook.Sheets[workbook.SheetNames[0]], { header: 1 });
      expect(rows.length).to.be.greaterThan(1);
      expect(rows[0][1]).to.eq('Action');
      expect(rows[0][13]).to.eq('Action code');
    });
    cy.screenshot('excel-audit-export');
  });

  it('imports a downloaded Chinese user template after switching to English', () => {
    cy.request({ url: '/api/import-templates/users?lang=zh-CN', encoding: 'binary' }).then(({ body }) => {
      template = XLSX.read(body, { type: 'binary' });
      expect(template.SheetNames).to.deep.eq(['用户', '填写说明']);
      const first = template.Sheets[template.SheetNames[0]];
      expect(first.A1.v).to.eq('用户名');
      XLSX.utils.sheet_add_aoa(first, [[`excel-${stamp}`, '模板用户', 'example-test-password', `template-${stamp}@example.invalid`, '1', '', 'enabled']], { origin: 'A2' });
      cy.writeFile('../.codex-artifacts/excel-export-review/users-import-template-zh-CN.xlsx', body, 'binary');
    });
    cy.visit('/admin/users');
    cy.get('[data-cy=btn-user-bulk-import]').click();
    cy.then(() => cy.get('.modal-card input[type=file]').attachFile({
      fileContent: XLSX.write(template, { type: 'base64', bookType: 'xlsx' }),
      fileName: 'translated-users.xlsx',
      mimeType: contentType,
      encoding: 'base64',
    }));
    cy.get('.modal-card tbody tr').should('have.length', 1).and('contain', '模板用户');
    cy.get('.modal-card-foot button.is-primary').should('not.be.disabled');
    cy.screenshot('excel-template-round-trip');
    cy.viewport(390, 844);
    cy.get('.modal-card-foot button.is-primary').scrollIntoView().should('be.visible');
    cy.screenshot('excel-template-round-trip-mobile');
    cy.intercept('POST', '/api/users/bulk').as('importTranslatedUsers');
    cy.get('.modal-card-foot button.is-primary').click();
    cy.wait('@importTranslatedUsers').its('response.statusCode').should('eq', 200);
    cy.contains('.users tbody tr', '模板用户').should('exist');
  });

  it('emails the self-service workbook to the customer through the isolated SMTP sink', () => {
    cy.then(() => cy.request('POST', `/subscription/export/${customerUUID}?lang=zh-CN`));
    cy.request('http://127.0.0.1:9475/api/v2/messages').then(({ body }) => {
      const message = body.items.find((item) => item.Content.Headers.To.join(' ').includes(`excel-${stamp}@example.invalid`));
      expect(message).to.not.eq(undefined);
      const parts = message.MIME.Parts;
      const attachment = parts.find((part) => (part.Headers['Content-Disposition'] || []).join(' ').includes('customer-data.xlsx'));
      expect(attachment).to.not.eq(undefined);
      expect(attachment.Headers['Content-Type'].join(' ')).to.contain(contentType);
      const workbook = XLSX.read(attachment.Body.replace(/\s/g, ''), { type: 'base64' });
      expect(workbook.SheetNames[0]).to.eq('客户资料');
      cy.writeFile('../.codex-artifacts/excel-export-review/self-service-zh-CN.xlsx', attachment.Body.replace(/\s/g, ''), 'base64');
    });
  });
});
