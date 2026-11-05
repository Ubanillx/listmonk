/* eslint-env mocha */
/* global cy, expect */
const smtp = (name, email) => ({
  name, from_email: email, enabled: true, host: 'host.docker.internal', port: 6125,
  auth_protocol: 'none', tls_type: 'none', daily_limit: 10,
});

describe('Campaign SMTP scope follows the owner and workspace', () => {
  let organizationID;
  let privateListID;
  let poolID;
  const senderPools = [];

  it('keeps public and private audiences on the same personal sender configuration', () => {
    cy.resetDB();
    cy.viewport(1200, 680);
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.loginAndVisit('/admin/campaigns/new');
    cy.request('PUT', '/api/profile/smtp', { smtp: [smtp('owner', 'owner@example.com')] });
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'SMTP workspace', manager_user_id: body.data.id,
    })).then(({ body }) => { organizationID = body.data.id; });
    cy.request('POST', '/api/customer-lists', { name: 'Public audience', type: 'pool', optin: 'single' })
      .then(({ body }) => { poolID = body.data.id; });
    cy.then(() => cy.request({
      method: 'POST', url: '/api/customer-lists', headers: { 'X-Listmonk-Organization-ID': organizationID },
      body: { name: 'Private audience', type: 'private', optin: 'single' },
    })).then(({ body }) => { privateListID = body.data.id; });
    cy.then(() => ['First sender pool', 'Second sender pool'].forEach((name, index) => {
      const headers = { 'X-Listmonk-Organization-ID': organizationID };
      cy.request({ method: 'POST', url: '/api/organizations/smtp-pools', headers, body: { name } })
        .then(({ body }) => {
          senderPools.push(body.data.id);
          cy.request({ method: 'PUT', url: `/api/organizations/smtp?pool_id=${body.data.id}`, headers,
            body: { smtp: [smtp(`org-${index}`, `org-${index}@example.com`)] } });
        });
    }));
    cy.then(() => cy.visit(`/admin/campaigns/new?customer_list_id=${poolID}`));
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'personal');
    cy.get('[data-cy=campaign-smtp-overview]').should('contain', 'owner@example.com').and('not.contain', 'org-0@example.com');
    cy.get('[data-cy=campaign-smtp-source]').select('organization');
    cy.get('[data-cy=campaign-smtp-workspace-required]').should('be.visible').and('contain', '切换');
    cy.get('[data-cy=campaign-smtp-overview]').should('not.exist');
    cy.get('[data-cy=campaign-smtp-pool]').should('not.exist');
    cy.get('input[name=from_email]').should('not.exist');
    cy.get('[data-cy=campaign-sender-section]').screenshot('organization-smtp-requires-workspace');
    cy.get('[data-cy=campaign-smtp-source]').select('personal');
    cy.get('[data-cy=campaign-smtp-overview]').should('contain', 'owner@example.com');
    cy.then(() => {
      cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(organizationID)));
      cy.setCookie('listmonk_workspace_organization_id', String(organizationID));
      cy.visit(`/admin/campaigns/new?customer_list_id=${privateListID}`);
    });
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'personal');
    cy.get('[data-cy=campaign-smtp-rate-limit]').clear().type('17');
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.then(() => cy.get(`[data-cy=campaign-audience-${poolID}]`).check());
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'personal');
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '17');
    cy.get('[data-cy=campaign-smtp-overview]').should('contain', 'owner@example.com');
    cy.get('[data-cy=campaign-smtp-source]').select('organization');
    cy.then(() => cy.get('[data-cy=campaign-smtp-pool]').select(String(senderPools[1])));
    cy.get('[data-cy=campaign-smtp-overview]').should('contain', 'org-1@example.com').and('not.contain', 'org-0@example.com');
    cy.get('[data-cy=campaign-smtp-rate-limit]').clear().type('37');
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.then(() => cy.get(`[data-cy=campaign-audience-${privateListID}]`).uncheck());
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.get('[data-cy=campaign-pool-scope]').select('all_organizations');
    cy.then(() => cy.get('[data-cy=campaign-smtp-pool]').should('have.value', String(senderPools[1])));
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '37');
    cy.get('[data-cy=campaign-smtp-overview]').should('contain', 'org-1@example.com').and('not.contain', 'owner@example.com');
    cy.get('[data-cy=campaign-smtp-pool]').should(($select) => {
      const section = $select[0].closest('[data-cy=campaign-sender-section]').getBoundingClientRect();
      expect($select[0].getBoundingClientRect().right).to.be.at.most(section.right);
    });
    cy.get('[data-cy=campaign-sender-section]').should(($section) => {
      const win = $section[0].ownerDocument.defaultView;
      expect($section[0].getBoundingClientRect().right).to.be.at.most(win.innerWidth);
      expect(win.document.documentElement.scrollWidth).to.be.at.most(win.innerWidth);
    });
    cy.screenshot('smtp-desktop-viewport', { capture: 'viewport' });
    cy.get('[data-cy=campaign-sender-section]').screenshot('public-audience-selected-organization-pool');
    cy.viewport(390, 844);
    cy.get('[data-cy=campaign-smtp-pool]').should('be.visible');
    cy.window().then((win) => expect(win.document.documentElement.scrollWidth).to.be.at.most(win.innerWidth));
    cy.get('[data-cy=campaign-sender-section]').screenshot('public-audience-selected-organization-pool-mobile');
  });

  it('preserves a saved second pool and custom rate on loading and saving', () => {
    cy.then(() => cy.request({
      method: 'POST', url: '/api/campaigns', headers: { 'X-Listmonk-Organization-ID': organizationID },
      body: {
        name: 'Saved SMTP selection', subject: 'SMTP selection', type: 'regular', content_type: 'plain', body: 'Body',
        customer_list_ids: [poolID], pool_scope: 'all_organizations', smtp_source: 'organization',
        smtp_pool_id: senderPools[1], smtp_rate_limit: 37,
      },
    })).then(({ body }) => cy.visit(`/admin/campaigns/${body.data.id}`));
    cy.then(() => cy.get('[data-cy=campaign-smtp-pool]').should('have.value', String(senderPools[1])));
    cy.get('[data-cy=campaign-smtp-rate-limit]').should('have.value', '37');
    cy.get('[data-cy=campaign-smtp-overview]').should('contain', 'org-1@example.com').and('not.contain', 'org-0@example.com');
    cy.get('input[name=subject]').clear().type('Preserved selection');
    cy.intercept('PUT', '/api/campaigns/*').as('saveSender');
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@saveSender').then(({ request, response }) => {
      expect(response.statusCode).to.eq(200);
      expect(request.body.smtp_pool_id).to.eq(senderPools[1]);
      expect(request.body.smtp_rate_limit).to.eq(37);
    });
    cy.reload();
    cy.then(() => cy.get('[data-cy=campaign-smtp-pool]').should('have.value', String(senderPools[1])));
  });
});
