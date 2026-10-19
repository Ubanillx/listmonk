/* eslint-env mocha */
/* global cy, Cypress, expect */

describe('Organization pool allocation workbench', () => {
  let pool;
  let org;
  let allocation;
  let contact;

  beforeEach(() => {
    expect(Cypress.config('baseUrl')).to.match(/^http:\/\/127\.0\.0\.1:9273$/);
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
    cy.loginAndVisit('/admin/pool-lists');
    cy.get('h1').should('be.visible');
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Workbench North', manager_user_id: body.data.id,
    })).then(({ body }) => { org = body.data; });
    cy.request('POST', '/api/customer-lists', {
      name: 'Workbench source', type: 'pool', optin: 'single', tags: [],
    })
      .then(({ body }) => { pool = body.data; });
    cy.then(() => cy.request('POST', '/api/pools/allocations', { pool_id: pool.id, organization_id: org.id, name: 'North allocation' }))
      .then(({ body }) => { allocation = body.data; });
    cy.then(() => cy.request('POST', `/api/pools/${pool.id}/contacts`, {
      customer_code: 'WB001', name: 'Workbench customer', email: 'private@example.com', reply_to: 'route@example.com',
    })).then(({ body }) => { contact = body.data; });
    cy.then(() => cy.request('POST', `/api/pools/allocations/${allocation.id}/manage-members`, { action: 'assign', contact_ids: [contact.id] }));
    cy.visit('/admin/pool-lists');
  });

  function open() {
    cy.contains('tbody tr', 'North allocation').find('[data-cy=btn-manage-allocation]').click();
    cy.get('[data-cy=allocation-workbench]').should('be.visible');
    cy.get('[data-cy=pool-current-organization]').should('contain', 'Workbench North');
    cy.get('[data-cy=allocation-members]').should('contain', 'WB001');
  }

  it('opens from the child and removes/restores only the relationship', () => {
    open();
    cy.get('[data-cy=allocation-workbench]').should('contain', 'Not granted');
    cy.contains('[data-cy=allocation-members] button', 'Remove from allocation').click();
    cy.get('[data-cy=allocation-confirm]').should('be.disabled');
    cy.get('[data-cy=allocation-reason]').type('Relationship QA');
    cy.get('[data-cy=allocation-confirm]').click();
    cy.get('[data-cy=allocation-members]').should('not.contain', 'WB001');
    cy.get('[data-cy=allocation-member-status]').select('removed');
    cy.get('[data-cy=allocation-members]').should('contain', 'Relationship QA');
    cy.contains('[data-cy=allocation-members] button', 'Restore allocation').click();
    cy.get('[data-cy=allocation-confirm]').click();
    cy.get('[data-cy=allocation-member-status]').select('active');
    cy.get('[data-cy=allocation-members]').should('contain', 'WB001');
    cy.then(() => cy.request(`/api/pools/${pool.id}/contacts`)).then(({ body }) => {
      expect(body.data.results[0].email).to.eq('private@example.com');
    });
    cy.then(() => cy.request(`/api/pools/${pool.id}/management-target?organization_id=${org.id}`))
      .its('body.data.delivery_allowed').should('eq', false);
  });

  it('saves allocation information, preserves deep links and guards drafts', () => {
    open();
    cy.get('[data-cy=allocation-tab-info]').click();
    cy.get('[data-cy=allocation-name]').clear().type('Updated North allocation');
    cy.get('[data-cy=allocation-save]').click();
    cy.get('[data-cy=allocation-workbench]').should('contain', 'Updated North allocation');
    cy.reload();
    cy.get('[data-cy=allocation-tab-info]').should('have.attr', 'aria-selected', 'true');
    cy.get('[data-cy=allocation-name]').clear().type('Unsaved allocation');
    cy.get('[data-cy=allocation-close]').click();
    cy.get('.dialog').should('contain', 'Discard unsaved');
    cy.get('.dialog .button').contains('Cancel').click();
    cy.get('[data-cy=allocation-name]').should('have.value', 'Unsaved allocation');
  });

  it('switches tabs in place without reloading the workbench or losing local state', () => {
    open();
    let modal;
    let organizationPanel;
    let height;
    const requests = {
      targets: 0, lists: 0, members: 0, events: 0,
    };
    cy.intercept('GET', '/api/pools/*/management-target*', (req) => { requests.targets += 1; req.continue(); });
    cy.intercept('GET', '/api/customer-lists*', (req) => { requests.lists += 1; req.continue(); });
    cy.intercept('GET', '/api/pools/allocations/*/manage-members*', (req) => { requests.members += 1; req.continue(); }).as('tabMembers');
    cy.intercept('GET', '/api/pools/allocations/*/events*', (req) => {
      requests.events += 1;
      req.continue((res) => { res.setDelay(700); });
    }).as('tabEvents');
    cy.get('[data-cy=allocation-workbench]').then(($modal) => {
      [modal] = $modal; height = modal.getBoundingClientRect().height;
    });
    cy.get('[data-cy=pool-target-organization-panel]').then(($panel) => { [organizationPanel] = $panel; });
    cy.get('[data-cy=allocation-search]').type('WB001');
    cy.get('.allocation-workbench__toolbar').submit();
    cy.wait('@tabMembers');
    cy.get('[data-cy=allocation-members] tbody .checkbox-cell label').click();
    cy.get('[data-cy=allocation-bulk]').should('contain', '(1)');
    cy.get('[data-cy=allocation-tab-info]').click();
    cy.get('[data-cy=allocation-name]').clear().type('Draft survives tab switches');
    cy.get('[data-cy=allocation-tab-history]').click();
    cy.get('[data-cy=allocation-workbench]').should(($modal) => {
      expect($modal[0]).to.eq(modal);
      expect($modal[0].getBoundingClientRect().height).to.be.closeTo(height, 1);
    });
    cy.wait('@tabEvents');
    cy.get('[data-cy=allocation-tab-members]').click();
    cy.get('[data-cy=allocation-search]').should('have.value', 'WB001');
    cy.get('[data-cy=allocation-members] tbody input[type=checkbox]').should('be.checked');
    cy.get('[data-cy=allocation-tab-members]').click();
    cy.get('[data-cy=allocation-tab-info]').click();
    cy.get('[data-cy=allocation-name]').should('have.value', 'Draft survives tab switches');
    cy.get('[data-cy=allocation-tab-history]').click();
    cy.get('[data-cy=pool-target-organization-panel]').should(($panel) => expect($panel[0]).to.eq(organizationPanel));
    cy.location('search').should('contain', 'manage_tab=history');
    cy.then(() => expect(requests).to.deep.eq({
      targets: 0, lists: 0, members: 1, events: 1,
    }));
    cy.get('[data-cy=allocation-tab-info]').click();
    cy.get('[data-cy=allocation-save]').click();
    cy.get('[data-cy=allocation-save]').should('not.have.class', 'is-loading');
    cy.get('[data-cy=allocation-tab-members]').click();
    cy.wait('@tabMembers');
    cy.get('[data-cy=allocation-search]').should('have.value', 'WB001');
    cy.get('[data-cy=allocation-workbench]').should(($modal) => expect($modal[0]).to.eq(modal));
  });

  it('keeps lifecycle and authorization independent and rebinds in disabled state', () => {
    open();
    cy.get('[data-cy=allocation-tab-info]').click();
    cy.get('[data-cy=allocation-toggle-state]').click();
    cy.get('[data-cy=allocation-confirm]').should('be.enabled').click();
    cy.get('[data-cy=allocation-workbench]').should('contain', 'Disabled').and('contain', 'Not granted');
    cy.get('[data-cy=allocation-detach]').click();
    cy.get('[data-cy=allocation-reason]').type('Unbinding QA');
    cy.get('[data-cy=allocation-confirm-organization]').type('Workbench North');
    cy.get('[data-cy=allocation-confirm]').click();
    cy.get('[data-cy=allocation-workbench]').should('contain', 'Unbound');
    cy.get('[data-cy=allocation-detach]').click();
    cy.get('[data-cy=allocation-confirm]').click();
    cy.get('[data-cy=allocation-workbench]').should('contain', 'Disabled').and('contain', 'Not granted');
    cy.then(() => cy.request(`/api/pools/allocations/${allocation.id}/manage`)).its('body.data.state').should('eq', 'disabled');
  });

  it('persists failed saves visibly and clears loading without losing draft', () => {
    open();
    cy.get('[data-cy=allocation-tab-info]').click();
    cy.get('[data-cy=allocation-name]').clear().type('Retained draft');
    cy.intercept('PUT', '/api/pools/allocations/*/manage', { statusCode: 409, body: { message: 'Allocation version conflict' } });
    cy.get('[data-cy=allocation-save]').click();
    cy.get('[data-cy=allocation-error]').should('contain', 'Allocation version conflict');
    cy.get('[data-cy=allocation-name]').should('have.value', 'Retained draft');
    cy.get('[data-cy=allocation-save]').should('not.have.class', 'is-loading').and('be.enabled');
  });

  it('fits mobile without a page-level overflow', () => {
    open();
    cy.viewport(390, 844);
    cy.get('[data-cy=allocation-workbench]').should('be.visible');
    cy.get('.allocation-workbench__mobile-picker').should('be.visible');
    cy.document().then((doc) => expect(doc.documentElement.scrollWidth).to.be.at.most(390));
    cy.get('[data-cy=allocation-tab-info]').click();
    cy.get('[data-cy=allocation-name]').should('be.visible');
  });

  function delegateProfile(read = true) {
    cy.intercept('GET', '/api/profile', (req) => req.continue((res) => {
      res.body.data.user_role = {
        id: 77,
        permissions: [
          'workspaces:personal', 'pools:allocation_platform_manage', 'audit:get', ...(read ? ['pools:get'] : []),
        ],
      };
    }));
  }

  it('shows cross-organization allocation controls without master or delivery actions', () => {
    delegateProfile();
    cy.visit('/admin/pool-lists');
    open();
    cy.get('[data-cy=btn-new]').should('not.exist');
    cy.get('[data-cy=btn-edit]').should('not.exist');
    cy.get('[data-cy=pool-delivery-grant]').should('contain', 'Not granted').find('button').should('not.exist');
    cy.get('[data-cy=allocation-tab-info]').click();
    cy.get('[data-cy=allocation-detach]').should('be.visible');
    cy.get('[data-cy=allocation-tab-members]').click();
    cy.get('[data-cy=allocation-members]').should('not.contain', 'private@example.com').and('contain', '***');
  });

  it('does not request customer rows without pool reading permission', () => {
    delegateProfile(false);
    const requests = [];
    cy.intercept('GET', '/api/pools/allocations/*/manage-members*', (req) => { requests.push(req.url); req.continue(); });
    cy.visit('/admin/pool-lists');
    cy.contains('tbody tr', 'North allocation').find('[data-cy=btn-manage-allocation]').click();
    cy.get('[data-cy=allocation-tab-info]').should('have.attr', 'aria-selected', 'true');
    cy.get('[data-cy=allocation-tab-members]').click();
    cy.get('[data-cy=allocation-members]').should('not.exist');
    cy.get('[data-cy=allocation-add]').should('not.exist');
    cy.then(() => expect(requests).to.have.length(0));
  });

  it('ignores a late organization response without switching the workspace', () => {
    let second;
    let workspace;
    cy.request('/api/workspace').then(({ body }) => { workspace = body.data.organization_id; });
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Workbench South', manager_user_id: body.data.id,
    })).then(({ body }) => { second = body.data; });
    cy.visit('/admin/pool-lists');
    open();
    cy.then(() => {
      cy.intercept(
        'GET',
        `/api/pools/${pool.id}/management-target?organization_id=${second.id}`,
        (req) => req.continue((res) => { res.setDelay(800); }),
      ).as('slowSouth');
      cy.get(`[data-organization-id="${second.id}"]`).click();
      cy.get(`[data-organization-id="${org.id}"]`).click();
    });
    cy.wait('@slowSouth');
    cy.get('[data-cy=pool-current-organization]').should('contain', 'Workbench North');
    cy.get('[data-cy=allocation-members]').should('contain', 'WB001');
    cy.request('/api/workspace').then(({ body }) => expect(body.data.organization_id).to.eq(workspace));
  });

  it('renders the selected Chinese design with removed contacts and a separate authorization summary', () => {
    cy.viewport(1442, 1091);
    cy.then(() => cy.request('POST', `/api/pools/allocations/${allocation.id}/manage-members`, {
      action: 'remove', contact_ids: [contact.id], reason: '转入私有',
    }));
    delegateProfile();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.visit('/admin/pool-lists');
    cy.contains('tbody tr', 'North allocation').find('[data-cy=btn-manage-allocation]').click();
    cy.get('[data-cy=allocation-member-status]').select('removed');
    cy.get('[data-cy=allocation-members]').should('contain', '转入私有');
    cy.get('[data-cy=allocation-add]').then(($add) => {
      cy.get('[data-cy=allocation-close]').then(($close) => {
        expect($add[0].getBoundingClientRect().left).to.be.lessThan($close[0].getBoundingClientRect().left - 200);
      });
    });
    cy.screenshot('allocation-workbench-desktop', { capture: 'viewport' });
    cy.viewport(390, 844);
    cy.get('[data-cy=allocation-members]').scrollIntoView().should('be.visible');
    cy.get('[data-cy=allocation-workbench]').then(($modal) => {
      expect($modal[0].scrollWidth).to.be.at.most($modal[0].clientWidth);
      const rect = $modal[0].getBoundingClientRect();
      expect(rect.top).to.be.at.least(0);
      expect(rect.bottom).to.be.at.most(844);
    });
    cy.get('[data-cy=allocation-close]').should('be.visible');
    cy.screenshot('allocation-workbench-mobile', { capture: 'viewport' });
  });
});
