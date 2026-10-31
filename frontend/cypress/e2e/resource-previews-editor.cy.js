/* eslint-env mocha */
/* global cy, expect */

describe('Resource previews and localized visual editor', () => {
  let organizationID;
  let secondID;
  let managerID;
  const frame = () => cy.get('#visual-editor').its('0.contentDocument.body').should('not.be.empty').then(cy.wrap);
  const pending = (name, total = 1) => ({
    data: {
      counts: total ? [{ kind: 'campaigns', count: total }] : [],
      results: total ? [{
        kind: 'campaigns', id: 91, name, original_owner_user_id: managerID, original_owner_name: 'Former owner', pending_at: '2026-10-01T00:00:00Z',
      }] : [],
      total,
      page: 1,
      per_page: 20,
    },
  });

  before(() => {
    cy.resetDB();
    cy.loginAndVisit('/admin/organizations/manage');
    cy.request('/api/profile').then(({ body }) => {
      managerID = body.data.id;
      cy.request('POST', '/api/organizations', { name: 'Preview team', manager_user_id: managerID })
        .then((response) => { organizationID = response.body.data.id; });
      cy.request('POST', '/api/organizations', { name: 'Second preview team', manager_user_id: managerID })
        .then((response) => { secondID = response.body.data.id; });
    });
  });

  beforeEach(() => {
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
  });

  it('refreshes real cleanup conditions before confirming and cancels without a delete', () => {
    let orphanCalls = 0;
    cy.intercept('GET', '/api/maintenance/customers/orphan/preview*', (req) => {
      orphanCalls += 1;
      req.reply({ body: { data: { count: orphanCalls === 1 ? 5 : 7 } } });
    }).as('orphans');
    cy.intercept('GET', '/api/maintenance/customers/blocklisted/preview*', { statusCode: 500, body: { message: 'Preview failed' } });
    cy.intercept('GET', '/api/maintenance/analytics/*/preview*', { body: { data: { count: 3, views: 2, clicks: 1 } } });
    cy.intercept('GET', '/api/maintenance/subscriptions/unconfirmed/preview*').as('cutoff');
    cy.intercept('DELETE', '/api/maintenance/**', (req) => { throw new Error(`Unexpected deletion: ${req.url}`); });
    cy.visit('/admin/settings/maintenance');
    cy.wait('@cutoff').then(({ request }) => {
      const cutoff = new Date(new URL(request.url).searchParams.get('before_date'));
      cy.request('/api/config').then(({ body }) => {
        const time = new Intl.DateTimeFormat('en-GB', {
          timeZone: body.data.timezone, hourCycle: 'h23', hour: '2-digit', minute: '2-digit',
        });
        expect(time.format(cutoff)).to.eq('00:00');
      });
    });
    cy.get('[data-cy=maintenance-preview-customers]').should('contain', '5 位客户');
    cy.get('[data-cy=maintenance-preview-analytics]').should('contain', '浏览记录 2 条').and('contain', '点击记录 1 条');
    cy.get('.maintenance .button.is-danger').first().click();
    cy.get('.dialog').should('contain', '7 位客户').and('contain', '全平台');
    cy.get('.dialog header').should('contain', '确认清理');
    cy.contains('.dialog button', '取消').click();
    cy.get('.maintenance select').first().select('blocklisted');
    cy.get('[data-cy=maintenance-preview-customers]').should('contain', '暂时无法获取数量').and('not.contain', '7 位客户');
    cy.get('.maintenance .button.is-danger').first().should('be.disabled');
    cy.intercept('GET', '/api/maintenance/customers/blocklisted/preview*', { body: { data: { count: 0 } } });
    cy.contains('[data-cy=maintenance-preview-customers] button', '刷新预览').click();
    cy.get('[data-cy=maintenance-preview-customers]').should('contain', '0 位客户');
    cy.get('.maintenance .button.is-danger').first().should('be.disabled');
  });

  it('ignores a delayed previous organization preview, confirms the named recipient and preserves scope on cancel', () => {
    cy.intercept('GET', '/api/organizations/resources/pending*', (req) => {
      const old = Number(req.headers['x-listmonk-organization-id']) === organizationID;
      req.reply({ delay: old ? 1200 : 0, body: pending(old ? 'Old resource' : 'Current resource', old ? 50 : 1) });
    }).as('pending');
    cy.intercept('POST', '/api/organizations/resources/transfer', () => { throw new Error('Unexpected transfer'); });
    cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(organizationID)));
    cy.setCookie('listmonk_workspace_organization_id', String(organizationID));
    cy.visit('/admin/organizations/manage?tab=pending');
    cy.get('.section-mini select').select(String(secondID));
    cy.get('[data-cy=pending-resources]').should('contain', 'Current resource');
    cy.wait('@pending'); cy.wait('@pending');
    cy.get('[data-cy=pending-resources]').should('not.contain', 'Old resource').and('contain', '共 1 项');
    cy.get('select[data-cy=pending-recipient]').select(String(managerID));
    cy.get('[data-cy=pending-transfer]').click();
    cy.get('.dialog').should('contain', 'Second preview team').and('contain', '营销活动 1');
    cy.get('.dialog header').should('contain', '确认转移资源');
    cy.contains('.dialog button', '取消').click();
    cy.get('select[data-cy=pending-recipient]').should('have.value', String(managerID));
    cy.intercept('GET', '/api/organizations/resources/pending*', { body: pending('', 0) });
    cy.contains('[data-cy=pending-resources] button', '刷新预览').click();
    cy.get('[data-cy=pending-resources]').should('contain', '没有待转移资源');
    cy.get('[data-cy=pending-transfer]').should('be.disabled');
  });

  it('localizes controls, keeps content across locale changes and retries a failed save before reading it back', () => {
    cy.visit('/admin/campaigns/templates');
    cy.get('.templates .btn-new').click();
    cy.get('.template-modal-content input').first().clear().type('中文模板回归');
    cy.get('.template-modal-content select').first().select('campaign_visual');
    frame().find('[role=tab]').should('contain', '整体样式').and('contain', '内容设置');
    frame().find('button[aria-label="导入 JSON"]').click();
    frame().find('textarea').first().type('invalid');
    frame().should('contain', 'JSON 格式有误');
    frame().contains('button', '取消').click();
    const doc = { root: { type: 'EmailLayout', data: { childrenIds: ['text'] } }, text: { type: 'Text', data: { props: { text: '保留内容 {{ .Customer.Name }}' } } } };
    frame().find('button[aria-label="导入 JSON"]').click();
    frame().find('textarea').first().clear()
      .type(JSON.stringify(doc), { parseSpecialCharSequences: false });
    frame().contains('button', /^导入$/).click();
    frame().should('contain', '保留内容 {{ .Customer.Name }}');
    frame().find('button[aria-label="JSON 源码"]').click();
    frame().find('pre').invoke('text').then((text) => expect(JSON.parse(text).text.data.props.text).to.eq('保留内容 {{ .Customer.Name }}'));
    cy.get('#visual-editor').its('0.contentWindow.EmailBuilder').then((builder) => builder.setLocale('en'));
    frame().find('[role=tab]').should('contain', 'Styles').and('contain', 'Inspect');
    frame().find('pre').should('contain', '保留内容 {{ .Customer.Name }}');
    cy.get('#visual-editor').its('0.contentWindow.EmailBuilder').then((builder) => builder.setLocale('zh-CN'));
    frame().find('button[aria-label="编辑"]').click();
    cy.viewport(390, 844);
    frame().find('button[aria-label="收起设置"]').click();
    frame().find('button[aria-label="展开设置"]').should('be.visible').click();
    frame().contains('整体样式').should('be.visible');
    frame().contains('button', '返回画布').should('be.visible').click();
    frame().find('button[aria-label="展开设置"]').should('be.visible');
    cy.document().then((document) => expect(document.documentElement.scrollWidth).to.be.at.most(390));
    cy.intercept({ method: 'POST', url: '/api/templates', times: 1 }, { statusCode: 500, body: { message: 'Save failed' } }).as('saveFailure');
    cy.get('.template-modal-content .modal-card-foot button').contains('保存').click();
    cy.wait('@saveFailure');
    frame().should('contain', '保留内容 {{ .Customer.Name }}');
    cy.get('.template-modal-content input').first().should('have.value', '中文模板回归');
    cy.intercept('POST', '/api/templates').as('saveTemplate');
    cy.get('.template-modal-content .modal-card-foot button').contains('保存').click();
    cy.wait('@saveTemplate').its('response.statusCode').should('eq', 200);
    cy.get('.template-modal-content').should('not.exist');
    cy.viewport(1440, 900);
    cy.reload();
    cy.contains('tr', '中文模板回归').find('[data-cy=btn-edit]').click();
    frame().should('contain', '整体样式').and('contain', '保留内容 {{ .Customer.Name }}');
    cy.get('.template-modal-content .modal-card-foot button').contains('取消').click();
  });
});
