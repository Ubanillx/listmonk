/* eslint-env mocha */
/* global cy, expect */

describe('Organization SMTP reply mailbox warning', () => {
  let organizationID;
  let campaignID;
  let mailboxID;
  let poolID;

  it('warns when a mailbox exists but is not selected, and updates with the selection', () => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.loginAndVisit('/admin/campaigns');
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Reply warning team', manager_user_id: body.data.id,
    })).then(({ body }) => { organizationID = body.data.id; });
    cy.request('POST', '/api/customer-lists', { name: 'Warning public audience', type: 'pool', optin: 'single' })
      .then(({ body }) => { poolID = body.data.id; });
    cy.then(() => {
      const headers = { 'X-Listmonk-Organization-ID': organizationID };
      cy.request({
        method: 'POST', url: '/api/profile/reply-mailboxes', headers,
        body: { email: 'replies@example.invalid', ai_enabled: false, is_default: false },
      }).then(({ body }) => { mailboxID = body.data.id; });
      cy.request({ method: 'POST', url: '/api/organizations/smtp-pools', headers, body: { name: 'Warning sender pool' } })
        .then(({ body: smtpPool }) => cy.request({
          method: 'POST', url: '/api/customer-lists', headers,
          body: { name: 'Warning private audience', type: 'private', optin: 'single' },
        }).then(({ body: list }) => cy.request({
          method: 'POST', url: '/api/campaigns', headers,
          body: {
            name: 'Missing customer replies', subject: 'Reply warning', type: 'regular', content_type: 'plain', body: 'Body',
            customer_list_ids: [list.data.id], smtp_source: 'organization', smtp_pool_id: smtpPool.data.id,
          },
        }))).then(({ body }) => { campaignID = body.data.id; });
      cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(organizationID)));
      cy.setCookie('listmonk_workspace_organization_id', String(organizationID));
    });
    cy.then(() => cy.visit(`/admin/campaigns/${campaignID}`));
    cy.get('[data-cy=campaign-reply-mailbox-warning]').should('be.visible').and('have.attr', 'role', 'alert')
      .and('contain', '未设置回信邮箱').and('contain', '客户回信将无法接收');
    cy.get('[data-cy=campaign-reply-mailbox-warning]').should(($warning) => {
      expect($warning[0].scrollWidth).to.be.at.most($warning[0].clientWidth);
    });
    cy.window().then((win) => expect(win.document.documentElement.scrollWidth).to.be.at.most(win.innerWidth));
    cy.get('[data-cy=campaign-reply-mailbox]').should('not.be.disabled')
      .and('have.attr', 'aria-describedby', 'campaign-reply-mailbox-warning')
      .and('have.css', 'border-top-color', 'rgb(217, 45, 32)');
    cy.get('[data-cy=campaign-sender-section]').screenshot('organization-reply-mailbox-warning-desktop');
    cy.get('[data-cy=campaign-smtp-source]').select('personal');
    cy.get('[data-cy=campaign-reply-mailbox-warning]').should('not.exist');
    cy.get('[data-cy=campaign-smtp-source]').select('organization');
    cy.get('[data-cy=campaign-reply-mailbox-warning]').should('be.visible');
    cy.then(() => cy.get('[data-cy=campaign-reply-mailbox]').select(String(mailboxID)));
    cy.get('[data-cy=campaign-reply-mailbox-warning]').should('not.exist');
    cy.get('[data-cy=campaign-reply-mailbox]').should('not.have.attr', 'aria-describedby');
    cy.get('[data-cy=campaign-reply-mailbox]').select('');
    cy.get('[data-cy=campaign-reply-mailbox-warning]').should('be.visible');
    cy.viewport(390, 844);
    cy.get('[data-cy=campaign-reply-mailbox-warning]').scrollIntoView().should('be.visible');
    cy.window().then((win) => expect(win.document.documentElement.scrollWidth).to.be.at.most(win.innerWidth));
    cy.get('[data-cy=campaign-sender-section]').screenshot('organization-reply-mailbox-warning-mobile');
    cy.viewport(1400, 950);
  });

  it('keeps the warning and red field visible on a completed read-only campaign', () => {
    cy.then(() => {
      cy.intercept('GET', `/api/campaigns/${campaignID}`, (req) => req.continue((res) => { res.body.data.status = 'finished'; }));
      cy.visit(`/admin/campaigns/${campaignID}`);
    });
    cy.get('[data-cy=campaign-reply-mailbox]').should('be.disabled').and('have.css', 'border-top-color', 'rgb(217, 45, 32)');
    cy.get('[data-cy=campaign-reply-mailbox-warning]').should('be.visible');
    cy.get('[data-cy=campaign-sender-section]').screenshot('organization-reply-mailbox-warning-readonly');
  });

  it('warns with no available mailboxes and keeps public-pool routing independent', () => {
    cy.intercept('GET', '/api/profile/reply-mailboxes', { body: { data: [] } });
    cy.then(() => cy.visit(`/admin/campaigns/${campaignID}`));
    cy.get('[data-cy=campaign-reply-mailbox]').should('be.disabled');
    cy.get('[data-cy=campaign-reply-mailbox-warning]').should('be.visible').and('contain', '组织邮件配置中添加');
    cy.then(() => cy.visit(`/admin/campaigns/new?customer_list_id=${poolID}`));
    cy.get('[data-cy=campaign-audience-trigger]').should('contain', 'Warning public audience');
    cy.get('[data-cy=campaign-smtp-source]').should('have.value', 'organization');
    cy.get('[data-cy=campaign-reply-mailbox]').should('not.exist');
    cy.get('[data-cy=campaign-reply-mailbox-warning]').should('not.exist');
    cy.get('[data-cy=campaign-pool-reply-priority]').should('be.visible');
  });
});
