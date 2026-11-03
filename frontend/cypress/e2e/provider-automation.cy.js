/* eslint-env mocha */
/* global cy, expect, Cypress */

const defaults = () => ({
  enabled: false, types: [1, 3, 6, 8, 9], schedule: 'daily', daily_time: '02:00', interval_hours: 24, lookback_days: 7, excluded_emails: [],
});

describe('Platform delivery provider automation', () => {
  before(() => { cy.resetDB(); });
  beforeEach(() => {
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
  });

  it('persists independent disabled rules through the real API and requires previews before enabling', () => {
    cy.loginAndVisit('/admin/delivery-provider');
    cy.get('[data-cy=provider-tab-automation]').click();
    cy.get('[data-cy=provider-tab-automation]').should('be.visible');
    cy.get('[data-cy=automation-private]').should('contain', '全平台个人及组织');
    cy.get('[data-cy=automation-pool]').should('contain', '所有组织');
    cy.request('/api/delivery-provider/automation').then(({ body }) => {
      expect(body.data.scopes).to.have.length(2);
      body.data.scopes.forEach(({ rule }) => {
        expect(rule.config.enabled).to.equal(false);
        expect(rule.config.types).to.deep.equal([1, 3, 6, 8, 9]);
      });
    });
    cy.get('[data-cy=automation-private]').within(() => {
      cy.get('input[type=number]').last().clear().type('9');
      cy.contains('button', '保存规则').click();
      cy.contains('规则已保存').should('be.visible');
    });
    cy.reload();
    cy.get('[data-cy=automation-private] input[type=number]').last().should('have.value', '9');
    cy.get('[data-cy=automation-pool] input[type=number]').last().should('have.value', '7');
    cy.get('[data-cy=automation-private]').within(() => {
      cy.contains('label', '启用定时拉黑').click();
      cy.contains('button', '保存规则').should('be.disabled');
    });
    cy.request('/api/delivery-provider/automation').then(({ body }) => {
      const { rule } = body.data.scopes[0];
      cy.request({ method: 'PUT', url: '/api/delivery-provider/automation/private', failOnStatusCode: false,
        body: { version: rule.version, config: { ...rule.config, enabled: true } } }).its('status').should('eq', 409);
      cy.request({ method: 'PUT', url: '/api/delivery-provider/automation/private',
        body: { version: rule.version, config: defaults() } });
    });
  });

  it('previews, invalidates changed settings, confirms a manual run, and renders mobile details', () => {
    const rules = ['private', 'pool'].map((scope) => ({ rule: { scope, version: 1, config: defaults(), next_run_at: null }, runs: [] }));
    const counts = { addresses: 2, to_block: 1, already: 0, excluded: 0, unmatched: 1, skipped: 0 };
    const run = { id: 12, scope: 'private', version: 1, trigger: 'preview', status: 'preview', date_start: '2026-10-28', date_end: '2026-11-03', counts, created_at: '2026-11-03T04:00:00+08:00' };
    const item = { target_id: 4, name: 'Test customer', email: 'customer@example.test', outcome: 'to_block', evidence: { email: 'customer@example.test', types: [1, 9], count: 2 } };
    cy.intercept('GET', '/api/delivery-provider/automation', (req) => req.reply({ data: { scopes: rules, timezone: 'Asia/Shanghai' } }));
    cy.intercept('POST', '/api/delivery-provider/automation/private/preview', (req) => {
      expect(req.body.config.enabled).to.equal(false);
      rules[0].runs = [run]; req.reply({ data: { run, items: [item] } });
    }).as('preview');
    cy.intercept('GET', '/api/delivery-provider/automation/private/runs/12*', { data: [item] });
    cy.intercept('POST', '/api/delivery-provider/automation/private/run', (req) => {
      expect(req.body.preview_id).to.equal(12);
      rules[0].runs = [{ ...run, status: 'success', trigger: 'manual' }];
      req.reply({ data: { run: rules[0].runs[0], items: [{ ...item, outcome: 'blocked' }] } });
    }).as('run');
    cy.loginAndVisit('/admin/delivery-provider');
    cy.get('[data-cy=provider-tab-automation]').click();
    cy.get('[data-cy=automation-private]').within(() => {
      cy.contains('button', '预览').click(); cy.wait('@preview');
      cy.contains('.automation-preview', '待拉黑客户').should('be.visible');
      cy.contains('button', '立即执行一次').should('not.be.disabled');
      cy.get('textarea').type('except@example.test');
      cy.contains('button', '立即执行一次').should('be.disabled');
      cy.get('textarea').clear();
      cy.contains('button', '立即执行一次').click();
    });
    cy.get('.dialog').should('contain', '1 个待拉黑客户').within(() => { cy.contains('button', '立即执行一次').click(); });
    cy.wait('@run');
    cy.get('[data-cy=automation-private]').should('contain', '本次执行已完成');
    cy.screenshot('automation-desktop', { capture: 'fullPage' });
    cy.viewport(390, 844);
    cy.get('[data-cy=provider-tab-automation]').should('be.visible');
    cy.get('[data-cy=automation-private]').within(() => { cy.contains('button', '查看明细').click(); });
    cy.get('.automation-detail').should('be.visible').and('contain', 'customer@example.test');
    cy.get('.automation-detail').contains('button', '关闭').click();
    cy.document().then((doc) => { expect(doc.documentElement.scrollWidth).to.be.at.most(390); });
    cy.screenshot('automation-mobile', { capture: 'fullPage' });
  });

  it('keeps failed preview inputs and denies delegated readers every automation route', () => {
    cy.loginAndVisit('/admin/delivery-provider');
    cy.get('[data-cy=provider-tab-automation]').click();
    cy.intercept('POST', '/api/delivery-provider/automation/private/preview', { statusCode: 502, body: { message: 'Provider unavailable' } });
    cy.get('[data-cy=automation-private]').within(() => {
      cy.get('textarea').type('keep@example.test'); cy.contains('button', '预览').click();
      cy.get('[role=alert]').should('contain', 'Provider unavailable');
      cy.get('textarea').should('have.value', 'keep@example.test');
    });
    cy.intercept('GET', '/api/profile', { data: { id: 77, user_role: { id: 77, permissions: ['delivery_provider:get', 'workspaces:personal'] }, customer_list_roles: [] } });
    cy.reload();
    cy.get('[data-cy=provider-tab-automation]').should('not.exist');
  });
});
