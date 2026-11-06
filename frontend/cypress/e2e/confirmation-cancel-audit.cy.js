describe('Confirmation cancellation audit', () => {
  let organizationID;
  let primaryPoolID;
  let backupPoolID;
  const input = (name) => `[data-cy=reply-mailbox-${name}] input, input[data-cy=reply-mailbox-${name}]`;
  const cancel = () => {
    cy.contains('.dialog .modal-card-foot button', '取消').click();
    cy.get('.dialog').should('not.exist');
  };
  const workspaceHeaders = () => ({ 'X-Listmonk-Organization-ID': organizationID });
  const openOrganization = (tab) => {
    cy.window().then((win) => win.localStorage.setItem('listmonk.workspace.organizationId', String(organizationID)));
    cy.then(() => cy.setCookie('listmonk_workspace_organization_id', String(organizationID)));
    cy.visit(`/admin/organizations/manage?tab=${tab}`);
    cy.get('.section-mini select').should('have.value', String(organizationID));
    cy.get(`#${tab}-content`).should('be.visible');
  };
  const assertNoMailboxWrites = () => {
    cy.get('@mailboxUpdate.all').should('have.length', 0);
    cy.get('@mailboxDisable.all').should('have.length', 0);
    cy.get('@mailboxDelete.all').should('have.length', 0);
  };

  beforeEach(() => {
    cy.clearCookies();
    cy.clearLocalStorage();
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.loginAndVisit('/admin/user/profile');
    cy.get('.user-profile').should('be.visible');
    cy.get('#app').should(($app) => expect($app[0].__vue__.$utils).to.exist);
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Confirmation QA organization', manager_user_id: body.data.id,
    })).then(({ body }) => { organizationID = body.data.id; });
  });

  it('cancels organization bounce opt-in for every policy and leaves the saved configuration unchanged', () => {
    let policyAction = '';
    cy.then(() => cy.request({
      method: 'POST', url: '/api/profile/reply-mailboxes', headers: workspaceHeaders(),
      body: { email: 'organization-replies@example.invalid', scan_enabled: false, ai_enabled: false, bounce_enabled: false },
    }));
    cy.intercept('GET', '/api/profile/reply-mailboxes', (req) => req.continue((res) => {
      res.body.data.forEach((row) => { row.bounce_policy = { enabled: true, threshold: 2, action: policyAction }; });
    }));
    cy.intercept('PUT', '/api/profile/reply-mailboxes/*').as('mailboxUpdate');
    cy.intercept('DELETE', '/api/profile/reply-mailboxes/*').as('mailboxDisable');
    cy.intercept('POST', '/api/profile/reply-mailboxes/*/delete').as('mailboxDelete');
    ['', 'delete', 'blocklist'].forEach((action, index) => {
      cy.then(() => { policyAction = action; });
      if (index === 0) openOrganization('reply-mailboxes');
      else cy.reload();
      cy.get('[data-cy=reply-mailbox-scan]').click();
      cy.get(input('name')).clear().type(`Draft ${index}`);
      cy.get('[data-cy=reply-mailbox-bounce]').click();
      cancel();
      cy.get('[data-cy=reply-mailbox-bounce] input').should('not.be.checked');
      cy.get('[data-cy=reply-mailbox-ai] input').should('not.be.checked');
      cy.get('[data-cy=reply-mailbox-scan] input').should('be.checked');
      cy.get(input('name')).should('have.value', `Draft ${index}`);
      cy.get('[data-cy=reply-mailbox-no-processing]').should('be.visible');
      cy.get('[data-cy=reply-mailbox-bounce]').click();
      cy.get('body').type('{esc}');
      cy.get('.dialog').should('not.exist');
      cy.get('[data-cy=reply-mailbox-bounce] input').should('not.be.checked');
    });
    assertNoMailboxWrites();
    cy.reload();
    cy.get('[data-cy=reply-mailbox-scan] input').should('not.be.checked');
    cy.then(() => cy.request({ url: '/api/profile/reply-mailboxes', headers: workspaceHeaders() }))
      .its('body.data.0').should('include', { scan_enabled: false, bounce_enabled: false, ai_enabled: false, name: '' });
  });

  it('cancels organization mailbox disabling and deletion without changing the mailbox or its draft', () => {
    cy.then(() => cy.request({
      method: 'POST', url: '/api/profile/reply-mailboxes', headers: workspaceHeaders(),
      body: { email: 'organization-keep@example.invalid', scan_enabled: false },
    }));
    openOrganization('reply-mailboxes');
    cy.intercept('PUT', '/api/profile/reply-mailboxes/*').as('mailboxUpdate');
    cy.intercept('DELETE', '/api/profile/reply-mailboxes/*').as('mailboxDisable');
    cy.intercept('POST', '/api/profile/reply-mailboxes/*/delete').as('mailboxDelete');
    cy.get(input('name')).type('Retain unsaved name');
    cy.contains('.reply-card-header button', '停用').click();
    cancel();
    cy.get('[data-cy=reply-mailbox-delete]').click();
    cancel();
    cy.get(input('name')).should('have.value', 'Retain unsaved name');
    cy.get('.reply-mailbox-card').should('have.length', 1);
    assertNoMailboxWrites();
    cy.reload();
    cy.contains('.reply-mailbox-card .tag', '回信地址可用').should('be.visible');
    cy.get(input('name')).should('have.value', '');
  });

  it('keeps organization SMTP drafts and enabled state when cancelling sender deletion and pool actions', () => {
    ['Primary confirmation pool', 'Backup confirmation pool'].forEach((name, index) => {
      cy.then(() => cy.request({
        method: 'POST', url: '/api/organizations/smtp-pools', headers: workspaceHeaders(), body: { name },
      })).then(({ body }) => { if (index === 0) primaryPoolID = body.data.id; else backupPoolID = body.data.id; });
    });
    cy.then(() => cy.request({
      method: 'PUT', url: `/api/organizations/smtp?pool_id=${primaryPoolID}`, headers: workspaceHeaders(),
      body: { smtp: [{ name: 'saved-organization-sender', enabled: true, host: 'smtp.example.invalid', port: 465,
        auth_protocol: 'none', tls_type: 'TLS', from_email: 'sender@example.invalid' }] },
    }));
    openOrganization('smtp');
    cy.intercept('DELETE', '/api/organizations/smtp/*').as('deleteSender');
    cy.intercept('DELETE', '/api/organizations/smtp-pools/*').as('deletePool');
    cy.intercept('POST', '/api/organizations/smtp-pools').as('createPool');
    cy.intercept('PUT', '/api/organizations/smtp*').as('saveSMTP');
    cy.get('.smtp-card input[type=text]').first().clear().type('Retain SMTP draft');
    cy.get('.smtp-enabled-field .switch').click();
    cy.contains('.smtp-card-actions button', '删除').click();
    cancel();
    cy.contains('.smtp-pool-option', 'Backup confirmation pool').click();
    cancel();
    cy.contains('.smtp-pool-option.is-active', 'Primary confirmation pool').should('be.visible');
    cy.get('.smtp-pool-create input').type('Cancelled new pool');
    cy.get('.smtp-pool-create button').click();
    cancel();
    cy.get('.smtp-pool-create input').should('have.value', 'Cancelled new pool');
    cy.get('.smtp-card input[type=text]').first().should('have.value', 'Retain SMTP draft');
    cy.get('.smtp-enabled-field input').should('not.be.checked');
    cy.get('@deleteSender.all').should('have.length', 0);
    cy.get('@createPool.all').should('have.length', 0);
    cy.get('@saveSMTP.all').should('have.length', 0);
    cy.reload();
    cy.get('.smtp-card input[type=text]').first().should('have.value', 'saved-organization-sender');
    cy.get('.smtp-enabled-field input').should('be.checked');
    cy.contains('.smtp-pool-option', 'Backup confirmation pool').click();
    cy.get('.smtp-empty-state').should('be.visible');
    cy.get('.smtp-pool-delete').click();
    cancel();
    cy.get('@deletePool.all').should('have.length', 0);
    cy.then(() => cy.request({ url: '/api/organizations/smtp-pools', headers: workspaceHeaders() }))
      .then(({ body }) => expect(body.data.map((pool) => pool.id)).to.include.members([primaryPoolID, backupPoolID]));
  });

  it('closes shared input prompts on cancellation without invoking confirmation', () => {
    const confirmed = cy.stub().as('promptConfirmed');
    const cancelled = cy.stub().as('promptCancelled');
    [null, undefined, cancelled].forEach((onCancel) => {
      cy.window().then((win) => {
        win.document.querySelector('#app').__vue__.$utils.prompt('取消输入验证', {}, confirmed, onCancel);
      });
      cy.get('.dialog input').type('Keep this unsubmitted');
      cancel();
    });
    cy.get('@promptConfirmed').should('not.have.been.called');
    cy.get('@promptCancelled').should('have.been.calledOnce');
  });

  it('settles cancelled settings navigation, retains the draft and permits a later confirmed navigation', () => {
    let navigationSettled = false;
    cy.visit('/admin/settings');
    cy.get('.b-tabs nav a').first().click();
    cy.get('input[name="app.site_name"]').clear().type('Retained settings draft');
    cy.intercept('PUT', '/api/settings').as('saveSettings');
    cy.window().then((win) => {
      win.document.querySelector('#app').__vue__.$router.push('/customers').then(
        () => { navigationSettled = true; },
        () => { navigationSettled = true; },
      );
    });
    cancel();
    cy.then(() => null).should(() => expect(navigationSettled, 'cancelled navigation completed').to.equal(true));
    cy.location('pathname').should('eq', '/admin/settings');
    cy.get('input[name="app.site_name"]').should('have.value', 'Retained settings draft');
    cy.get('@saveSettings.all').should('have.length', 0);
    cy.window().then((win) => { win.document.querySelector('#app').__vue__.$router.push('/customers').catch(() => {}); });
    cy.contains('.dialog .modal-card-foot button', '确认').click();
    cy.location('pathname').should('eq', '/admin/customers');
  });

  it('restores the mobile allocation organization picker on cancel while keeping the current draft', () => {
    let secondID;
    let poolID;
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Second confirmation organization', manager_user_id: body.data.id,
    })).then(({ body }) => { secondID = body.data.id; });
    cy.request('POST', '/api/customer-lists', { name: 'Confirmation pool source', type: 'pool', optin: 'single', tags: [] })
      .then(({ body }) => { poolID = body.data.id; });
    cy.then(() => cy.request('POST', '/api/pools/allocations', {
      pool_id: poolID, organization_id: organizationID, name: 'Confirmation allocation',
    }));
    cy.visit('/admin/pool-lists');
    cy.contains('tbody tr', 'Confirmation allocation').find('[data-cy=btn-manage-allocation]').click();
    cy.get('[data-cy=allocation-tab-info]').click();
    cy.get('[data-cy=allocation-name]').clear().type('Retained allocation draft');
    cy.viewport(390, 844);
    cy.intercept('GET', '/api/pools/*/management-target*').as('switchTarget');
    cy.intercept('PUT', '/api/pools/allocations/*/manage').as('saveAllocation');
    cy.then(() => cy.get('.allocation-workbench__mobile-picker select').select(String(secondID)));
    cancel();
    cy.then(() => cy.get('.allocation-workbench__mobile-picker select').should('have.value', String(organizationID)));
    cy.get('[data-cy=pool-current-organization]').should('contain', 'Confirmation QA organization');
    cy.get('[data-cy=allocation-name]').should('have.value', 'Retained allocation draft');
    cy.get('@switchTarget.all').should('have.length', 0);
    cy.get('@saveAllocation.all').should('have.length', 0);
    cy.then(() => cy.get('.allocation-workbench__mobile-picker select').select(String(secondID)));
    cy.get('body').type('{esc}');
    cy.get('.dialog').should('not.exist');
    cy.then(() => cy.get('.allocation-workbench__mobile-picker select').should('have.value', String(organizationID)));
    cy.then(() => cy.get('.allocation-workbench__mobile-picker select').select(String(secondID)));
    cy.contains('.dialog .modal-card-foot button', '放弃').click();
    cy.get('[data-cy=pool-current-organization]').should('contain', 'Second confirmation organization');
    cy.wait('@switchTarget');
    cy.get('[data-cy=allocation-workbench]').should('be.visible');
  });
});
