/* eslint-env mocha */
/* global cy, expect */

describe('Public-pool reply routing', () => {
  it('imports reply emails, shows them in both contact views and saves campaign priority', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/customers/import');
    let orgID;
    let poolID;
    let allocationListID;
    let campaignID;
    let privateListID;
    let replyMailboxID;
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Reply routing team', manager_user_id: body.data.id,
    })).then(({ body }) => { orgID = body.data.id; });
    cy.request('POST', '/api/customer-lists', { name: 'Reply routing pool', type: 'pool', optin: 'single' })
      .then(({ body }) => { poolID = body.data.id; });
    cy.then(() => cy.request('POST', '/api/pools/allocations', {
      pool_id: poolID, organization_id: orgID, name: 'Reply routing allocation',
    })).then(({ body }) => { allocationListID = body.data.list_id; });
    cy.visit('/admin/customers/import');
    cy.get('[data-cy=import-tab-pool]').click();
    cy.get('.customer_list-selector input').type('Reply routing pool');
    cy.contains('.customer_list-selector .autocomplete a', 'Reply routing pool').click();
    const fileContent = '客户编号,姓名,邮箱,分配部门,回信邮箱\n'
      + 'REPLY-1,Reply customer,customer@example.com,Reply routing team,reply@example.com\n'
      + 'REPLY-2,Second customer,second@example.com,Reply routing team,team@example.com\n';
    cy.get('input[type=file]').attachFile({ fileContent, fileName: 'pool-reply.csv', mimeType: 'text/csv' });
    cy.get('[data-cy=import-map-reply-to]').should('have.value', '回信邮箱');
    cy.intercept('POST', '/api/import/customers').as('importReply');
    cy.get('[data-cy=btn-upload-import]').click();
    cy.wait('@importReply').then(({ response }) => {
      expect(response.statusCode, JSON.stringify(response.body)).to.eq(200);
      expect(response.body.data.created).to.eq(2);
    });
    cy.then(() => cy.visit(`/admin/pool-lists/${poolID}/contacts`));
    cy.contains('[data-cy=pool-contacts-table] tbody tr', 'Reply customer').should('contain', 'reply@example.com');
    cy.then(() => cy.visit(`/admin/pool-lists/${allocationListID}/contacts`));
    cy.contains('[data-cy=pool-contacts-table] tbody tr', 'Reply customer').should('contain', 'reply@example.com');
    cy.then(() => cy.request(`/api/customer-lists/${poolID}/pool-contacts/export`))
      .its('body').should('contain', 'reply_to').and('contain', 'reply@example.com');
    cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(orgID)));
    cy.then(() => cy.setCookie('listmonk_workspace_organization_id', String(orgID)));
    cy.request('PUT', '/api/profile/smtp', { smtp: [{ name: 'Reply test sender', enabled: true,
      host: 'host.docker.internal', port: 6125, auth_protocol: 'none', from_email: 'sender@example.com', tls_type: 'none' }] });
    cy.request('POST', '/api/customer-lists', { name: 'Private reply audience', type: 'private', optin: 'single' })
      .then(({ body }) => { privateListID = body.data.id; });
    cy.request('POST', '/api/profile/reply-mailboxes', { email: 'private-reply@example.com', ai_enabled: false })
      .then(({ body }) => { replyMailboxID = body.data.id; });
    cy.then(() => cy.visit(`/admin/campaigns/new?customer_list_id=${poolID}`));
    cy.get('[data-cy=campaign-smtp-source]').select('personal');
    cy.get('[data-cy=campaign-pool-reply-priority]').should('have.value', 'contact_first').select('organization_first');
    cy.get('[data-cy=campaign-pool-reply-priority] option:selected').should('have.text', 'Organization reply mailbox → List reply mailbox');
    cy.get('[data-cy=campaign-sender-section]').should('contain', 'Reply priority').and('not.contain', 'Customer reply email');
    cy.get('[data-cy=pool-routing-notice] summary').click();
    cy.get('[data-cy=pool-routing-notice]').should('have.attr', 'open');
    cy.get('[data-cy=pool-routing-notice]').should('contain', 'Reply routing pool');
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.then(() => cy.get(`[data-cy=campaign-audience-${poolID}]`).uncheck());
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.get('[data-cy=campaign-pool-reply-priority]').should('not.exist');
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.then(() => cy.get(`[data-cy=campaign-audience-${poolID}]`).check());
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.get('[data-cy=campaign-pool-reply-priority]').should('have.value', 'organization_first');
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.then(() => cy.get(`[data-cy=campaign-audience-${privateListID}]`).check());
    cy.get('[data-cy=campaign-audience-trigger]').click();
    cy.then(() => cy.get('[data-cy=campaign-reply-mailbox]').select(String(replyMailboxID)));
    cy.get('[data-cy=campaign-pool-reply-priority]').should('have.value', 'organization_first');
    cy.get('input[name=name]').type('Reply priority campaign');
    cy.get('input[name=subject]').type('Reply routing subject');
    cy.intercept('POST', '/api/campaigns').as('createReplyCampaign');
    cy.get('[data-cy=btn-continue]').click();
    cy.wait('@createReplyCampaign').then(({ request, response }) => {
      expect(request.body.reply_mailbox_id).to.eq(replyMailboxID);
      expect(response.statusCode).to.eq(200);
      expect(response.body.data.pool_reply_priority).to.eq('organization_first');
      campaignID = response.body.data.id;
    });
    cy.then(() => cy.visit(`/admin/campaigns/${campaignID}`));
    cy.get('[data-cy=campaign-pool-reply-priority]').should('have.value', 'organization_first').select('contact_first');
    cy.intercept('PUT', '/api/campaigns/*').as('updateReplyCampaign');
    cy.get('[data-cy=btn-save]').first().click();
    cy.wait('@updateReplyCampaign').its('response.body.data.pool_reply_priority').should('eq', 'contact_first');
    cy.reload();
    cy.then(() => cy.get('[data-cy=campaign-reply-mailbox]').should('have.value', String(replyMailboxID)));
    cy.get('[data-cy=campaign-pool-reply-priority]').should('have.value', 'contact_first');
    cy.then(() => cy.request(`/api/campaigns/${campaignID}/pool-send-status`)).then(({ body }) => {
      expect(body.data.ready, JSON.stringify(body.data)).to.eq(true);
      // Mixed audiences are scoped to this organization; per-organization
      // readiness entries belong only to all_organizations pool campaigns.
      expect(body.data.pool_scope).to.eq('organization');
      expect(body.data.organizations).to.deep.equal([]);
    });
    cy.then(() => cy.request({ method: 'PUT', url: `/api/campaigns/${campaignID}`,
      body: { pool_reply_priority: 'invalid' }, failOnStatusCode: false })).its('status').should('eq', 400);
  });
});
