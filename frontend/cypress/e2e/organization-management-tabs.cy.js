/* eslint-env mocha */
/* global cy, expect */

describe('Organization management navigation and refresh', () => {
  let organizationID;
  let secondID;
  let mailboxID;
  const selectTab = (tab) => cy.get(`#${tab}-label`).click();
  const assertTab = (tab) => {
    cy.location('search').should('contain', `tab=${tab}`);
    cy.get(`#${tab}-label`).parent().should('have.attr', 'aria-selected', 'true');
    cy.get(`#${tab}-content`).should('be.visible');
  };

  before(() => {
    cy.resetDB();
    cy.loginAndVisit('/admin/organizations/manage');
    cy.request('/api/profile').then(({ body }) => {
      const manager = body.data.id;
      cy.request('POST', '/api/organizations', { name: 'Tab refresh team', manager_user_id: manager })
        .then((response) => { organizationID = response.body.data.id; });
      cy.request('POST', '/api/organizations', { name: 'Second tab team', manager_user_id: manager })
        .then((response) => { secondID = response.body.data.id; });
    });
  });

  beforeEach(() => {
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(organizationID)));
    cy.then(() => cy.setCookie('listmonk_workspace_organization_id', String(organizationID)));
  });

  it('restores every tab after reload and supports browser history without losing other query parameters', () => {
    cy.visit('/admin/organizations/manage?tab=reply-forwarding&marker=keep');
    assertTab('reply-forwarding');
    ['members', 'invites', 'pending', 'reply-mailboxes', 'reply-forwarding', 'smtp', 'platform'].forEach((tab) => {
      selectTab(tab);
      assertTab(tab);
      cy.reload();
      assertTab(tab);
      cy.location('search').should('contain', 'marker=keep');
    });
    selectTab('invites');
    selectTab('reply-forwarding');
    cy.go('back');
    assertTab('invites');
    cy.go('forward');
    assertTab('reply-forwarding');
    cy.get('[data-cy=btn-refresh]').click();
    assertTab('reply-forwarding');
    cy.screenshot('organization-management-persistent-tab');
  });

  it('normalizes invalid tabs and keeps a valid tab when switching organizations', () => {
    cy.visit('/admin/organizations/manage?tab=unknown&marker=keep');
    assertTab('members');
    cy.location('search').should('contain', 'marker=keep');
    selectTab('invites');
    cy.then(() => cy.get('.section-mini select').select(String(secondID)));
    assertTab('invites');
  });

  it('automatically reloads invite creation, revocation and data changed outside the current tab', () => {
    cy.visit('/admin/organizations/manage?tab=invites');
    cy.get('#invites-content input').first().type('Fresh invite');
    cy.get('#invites-content button[type=submit]').click();
    cy.contains('#invites-content tr', 'Fresh invite').should('be.visible');
    assertTab('invites');
    cy.contains('#invites-content tr', 'Fresh invite').contains('button', 'Revoke').click();
    cy.contains('.dialog .modal-card-foot button', 'Ok').click();
    cy.contains('#invites-content tr', 'Fresh invite').should('contain', 'Revoked');
    selectTab('members');
    cy.then(() => cy.request({
      method: 'POST',
      url: '/api/organizations/invites',
      headers: { 'X-Listmonk-Organization-ID': organizationID },
      body: { name: 'External invite' },
    }));
    selectTab('invites');
    cy.contains('#invites-content tr', 'External invite').should('be.visible');
  });

  it('ignores a delayed response from the previous management organization', () => {
    cy.intercept('GET', '/api/organizations/invites', (req) => {
      const oldTarget = Number(req.headers['x-listmonk-organization-id']) === Number(organizationID);
      req.reply({ delay: oldTarget ? 1200 : 0, body: { data: [{ id: oldTarget ? 81 : 82, name: oldTarget ? 'Old target invite' : 'Current target invite' }] } });
    }).as('targetInvites');
    cy.visit('/admin/organizations/manage?tab=invites');
    cy.get('.section-mini select').should('have.value', String(organizationID));
    cy.then(() => cy.get('.section-mini select').select(String(secondID)));
    cy.contains('#invites-content tr', 'Current target invite').should('be.visible');
    cy.wait('@targetInvites');
    cy.wait('@targetInvites');
    cy.get('#invites-content').should('contain', 'Current target invite').and('not.contain', 'Old target invite');
    assertTab('invites');
  });

  it('refreshes unified mailbox choices after mailbox creation, update and deletion', () => {
    cy.visit('/admin/organizations/manage?tab=reply-mailboxes');
    cy.contains('.reply-mailboxes button', 'New').click();
    cy.get('[data-cy=reply-mailbox-email] input, input[data-cy=reply-mailbox-email]').type('tab-replies@example.com');
    cy.intercept('POST', '/api/profile/reply-mailboxes').as('createMailbox');
    cy.get('[data-cy=reply-mailbox-save]').click();
    cy.wait('@createMailbox').then(({ response }) => { mailboxID = response.body.data.id; });
    cy.get('[data-cy=org-unified-reply-mailbox-select] option').should('contain', 'tab-replies@example.com');
    cy.get('[data-cy=reply-mailbox-email] input, input[data-cy=reply-mailbox-email]').clear().type('updated-tab-replies@example.com');
    cy.get('[data-cy=reply-mailbox-save]').click();
    cy.get('[data-cy=org-unified-reply-mailbox-select] option').should('contain', 'updated-tab-replies@example.com');
    cy.then(() => cy.get(`[data-cy=org-unified-reply-mailbox-select] option[value="${mailboxID}"]`)
      .should(($option) => expect($option.text().trim()).to.eq('updated-tab-replies@example.com')));
    cy.get('[data-cy=reply-mailbox-delete]').click();
    cy.contains('.dialog .modal-card-foot button', 'Ok').click();
    cy.get('[data-cy=org-unified-reply-mailbox-select] option').should('not.contain', 'updated-tab-replies@example.com');
    cy.then(() => cy.request({
      url: '/api/profile/reply-mailboxes',
      headers: { 'X-Listmonk-Organization-ID': organizationID },
    })).then(({ body }) => {
      expect(body.data.map((row) => row.id)).not.to.include(mailboxID);
    });
    assertTab('reply-mailboxes');
  });

  it('reloads forwarding status after update and when revisiting the tab', () => {
    let status = 'active';
    cy.intercept('GET', '/api/organizations/reply-forwarding', (req) => {
      req.reply({
        body: {
          data: [{
            id: 99,
            source_email: 'member@example.com',
            mailbox_email: 'replies@example.com',
            target_email: 'manager@example.com',
            status,
          }],
        },
      });
    }).as('forwardRules');
    cy.intercept('PUT', '/api/organizations/reply-forwarding/99', (req) => {
      status = req.body.status;
      req.reply({ body: { data: true } });
    });
    cy.visit('/admin/organizations/manage?tab=reply-forwarding');
    cy.contains('#reply-forwarding-content button', 'Pause').click();
    cy.contains('#reply-forwarding-content button', 'Resume').should('be.visible');
    assertTab('reply-forwarding');
    selectTab('members');
    cy.then(() => { status = 'active'; });
    selectTab('reply-forwarding');
    cy.contains('#reply-forwarding-content button', 'Pause').should('be.visible');
  });

  it('refreshes SMTP counts after save and delete while retaining unsaved edits on tab changes', () => {
    let servers = [{
      id: 51, name: 'Saved sender', enabled: true, host: 'smtp.example.com', from_email: 'sender@example.com', port: 465,
    }];
    cy.intercept('GET', '/api/organizations/smtp-pools', (req) => req.reply({
      body: {
        data: [
          {
            id: 71, name: 'Primary pool', smtp_count: servers.length, enabled_count: servers.filter((row) => row.enabled).length,
          },
        ],
      },
    }));
    cy.intercept({ method: 'GET', pathname: '/api/organizations/smtp' }, (req) => req.reply({ body: { data: servers } }));
    cy.intercept('PUT', '/api/organizations/smtp*', (req) => {
      servers = req.body.smtp;
      req.reply({ body: { data: servers } });
    });
    cy.intercept('DELETE', '/api/organizations/smtp/51*', (req) => {
      servers = [];
      req.reply({ body: { data: true } });
    });
    cy.visit('/admin/organizations/manage?tab=smtp');
    cy.get('.smtp-pool-option .help').should('contain', '1/1');
    cy.get('.smtp-card input[type=text]').first().clear().type('Unsaved sender');
    selectTab('members');
    selectTab('smtp');
    cy.get('.smtp-card input[type=text]').first().should('have.value', 'Unsaved sender');
    cy.get('.smtp-enabled-field .switch').click();
    cy.get('.smtp-save-bar button').click();
    cy.get('.smtp-pool-option .help').should('contain', '0/1');
    cy.contains('.smtp-card-actions button', 'Delete').click();
    cy.contains('.dialog .modal-card-foot button', 'Ok').click();
    cy.get('.smtp-pool-option .help').should('contain', '0/0');
    assertTab('smtp');
  });
});
