/* eslint-env mocha */
/* global cy, expect */

describe('Campaign audience SMTP defaults', () => {
  let organizationID;
  let privateListID;
  let poolID;
  let smtpPoolID;

  it('defaults private audiences to personal accounts and public audiences to organization accounts', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.loginAndVisit('/admin/campaigns/new');
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'personal');
    cy.get('[data-cy=campaign-smtp-source] option[value=personal]').should('have.text', '个人邮箱账号 SMTP 轮询发送');
    cy.get('[data-cy=campaign-smtp-source] option[value=organization]').invoke('text').invoke('trim').should('eq', '组织邮箱账号 SMTP 轮询发送');
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Audience defaults team', manager_user_id: body.data.id,
    })).then(({ body }) => { organizationID = body.data.id; });
    cy.request('POST', '/api/customer-lists', { name: 'Default public audience', type: 'pool', optin: 'single' })
      .then(({ body }) => { poolID = body.data.id; });
    cy.then(() => cy.request({
      method: 'POST', url: '/api/customer-lists', headers: { 'X-Listmonk-Organization-ID': organizationID },
      body: { name: 'Default private audience', type: 'private', optin: 'single' },
    })).then(({ body }) => { privateListID = body.data.id; });
    cy.then(() => cy.request({
      method: 'POST', url: '/api/organizations/smtp-pools', headers: { 'X-Listmonk-Organization-ID': organizationID },
      body: { name: 'Default sender pool' },
    })).then(({ body }) => { smtpPoolID = body.data.id; });
    cy.then(() => {
      cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(organizationID)));
      cy.setCookie('listmonk_workspace_organization_id', String(organizationID));
      cy.visit(`/admin/campaigns/new?customer_list_id=${privateListID}`);
    });
    cy.get('[data-cy=campaign-audience-trigger]').should('contain', 'Default private audience');
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'personal');
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '5');
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.then(() => cy.get(`[data-cy=campaign-audience-${poolID}]`).check());
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'organization');
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '100');
    cy.get('[data-cy=campaign-smtp-source]').select('personal');
    cy.get('[data-cy=campaign-smtp-rate-limit]').clear().type('17');
    cy.get('[data-cy=campaign-smtp-overview]').should('be.visible');
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'personal');
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '17');
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.then(() => cy.get(`[data-cy=campaign-audience-${privateListID}]`).uncheck());
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'organization');
    cy.get('[data-cy=campaign-sender-section]').screenshot('public-audience-organization-smtp');
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.then(() => cy.get(`[data-cy=campaign-audience-${poolID}]`).uncheck());
    cy.then(() => cy.get(`[data-cy=campaign-audience-${privateListID}]`).check());
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'personal');
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '5');
    cy.get('[data-cy=campaign-sender-section]').screenshot('private-audience-personal-smtp');
    cy.viewport(390, 844);
    cy.get('[data-cy=campaign-smtp-source]').should('be.visible');
    cy.window().then((win) => expect(win.document.documentElement.scrollWidth).to.be.at.most(win.innerWidth));
    cy.get('[data-cy=campaign-sender-section]').screenshot('private-audience-personal-smtp-mobile');
    cy.viewport(1400, 950);
    cy.then(() => cy.visit(`/admin/campaigns/new?customer_list_id=${poolID}`));
    cy.get('[data-cy=campaign-audience-trigger]').should('contain', 'Default public audience');
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'organization');
    cy.window().then((win) => win.localStorage.removeItem('listmonk.workspace.organizationId'));
    cy.clearCookie('listmonk_workspace_organization_id');
    cy.then(() => cy.visit(`/admin/campaigns/new?customer_list_id=${poolID}`));
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'organization');
    cy.get('[data-cy=campaign-smtp-pool]').should('not.exist');
  });

  it('preserves a saved manual source and custom rate when loading and saving the campaign', () => {
    cy.then(() => {
      cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(organizationID)));
      cy.setCookie('listmonk_workspace_organization_id', String(organizationID));
      cy.request({
        method: 'POST', url: '/api/campaigns', headers: { 'X-Listmonk-Organization-ID': organizationID },
        body: {
          name: 'Saved manual SMTP', subject: 'Manual override', type: 'regular', content_type: 'plain', body: 'Body',
          customer_list_ids: [privateListID], smtp_source: 'organization', smtp_pool_id: smtpPoolID, smtp_rate_limit: 37,
        },
      }).then(({ body }) => cy.visit(`/admin/campaigns/${body.data.id}`));
    });
    cy.get('[data-cy=campaign-audience-trigger]').should('contain', 'Default private audience');
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'organization');
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '37');
    cy.get('input[name=subject]').clear().type('Preserved manual override');
    cy.intercept('PUT', '/api/campaigns/*').as('saveManualSource');
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@saveManualSource').then(({ request, response }) => {
      expect(response.statusCode).to.eq(200);
      expect(request.body.smtp_source).to.eq('organization');
      expect(request.body.smtp_rate_limit).to.eq(37);
    });
    cy.visit('/admin/user/profile');
    cy.get('.user-profile .b-tabs nav a').eq(1).click();
    cy.get('.personal-smtp-header h2').should('contain', '个人邮箱账号 SMTP 发送');
  });
});
