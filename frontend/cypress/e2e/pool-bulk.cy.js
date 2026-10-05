/* eslint-env mocha */
/* global cy, Cypress, expect */

describe('Public pool bulk actions', () => {
  const pools = [];
  let organizationID;

  function selectContact(code) {
    cy.contains('[data-cy=pool-contacts-table] tbody tr', code).find('.checkbox-cell .checkbox').click();
  }

  function confirmDialog() {
    cy.get('.dialog .is-primary').click();
  }

  beforeEach(() => {
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
  });

  before(() => {
    // Run against a fresh isolated Cypress database, never the development DB.
    expect(Cypress.config('baseUrl')).to.match(/^http:\/\/127\.0\.0\.1:92[78]3$/);
    cy.loginAndVisit('/admin/customers');
    cy.get('[data-cy=btn-new]').should('be.visible');
    cy.request('/api/profile').then(({ body }) => cy.request('POST', '/api/organizations', {
      name: 'Bulk QA organization', manager_user_id: body.data.id,
    })).then(({ body }) => { organizationID = body.data.id; });
    ['Alpha', 'Beta'].forEach((name) => {
      const fixture = { name };
      pools.push(fixture);
      cy.request('POST', '/api/customer-lists', {
        name: `Bulk ${name}`, type: 'pool', optin: 'single', tags: [],
      }).then(({ body }) => {
        fixture.id = body.data.id;
        return cy.request('POST', '/api/org-pool-allocations', {
          pool_id: fixture.id, organization_id: organizationID, name: `${name} allocation`,
        });
      }).then(({ body }) => {
        fixture.allocationID = body.data.id;
        fixture.allocationListID = body.data.list_id;
      });
      ['selected', 'untouched'].forEach((suffix) => {
        cy.then(() => cy.request('POST', `/api/customer-lists/${fixture.id}/pool-contacts`, {
          customer_code: `${name}-${suffix}`, name: `${name} ${suffix}`, email: `${name}-${suffix}@example.com`,
        })).then(({ body }) => { fixture[suffix] = body.data.id; });
      });
    });
  });

  ['en', 'zh-CN', 'zh-TW'].forEach((lang) => {
    it(`localizes the private customer bulk toolbar in ${lang}`, () => {
      cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = lang; }));
      cy.visit('/admin/customers');
      cy.get('thead .checkbox-cell .checkbox').click();
      const text = {
        en: ['Manage private customer lists', 'Delete', 'Blocklist'],
        'zh-CN': ['管理私域客户列表', '删除', '黑名单'],
        'zh-TW': ['管理私域客戶清單', '刪除', '黑名單'],
      }[lang];
      cy.get('[data-cy=btn-manage-customer_lists]').should('contain', text[0]);
      cy.get('[data-cy=btn-delete-customers]').should('contain', text[1]);
      cy.get('[data-cy=btn-manage-blocklist]').should('contain', text[2]);
      cy.get('[data-cy=btn-manage-customer_lists]').click();
      cy.get('[data-cy=bulk-affected-count]').should('contain', '2');
      cy.get('.modal .modal-card-title, .modal h4').should('contain', text[0]);
    });
  });

  it('exports only selected memberships and rejects malformed selections', () => {
    const [alpha, beta] = pools;
    cy.request(`/api/pools/contacts/export?contact=${alpha.id}:${alpha.selected}&contact=${beta.id}:${beta.selected}`).then(({ body }) => {
      expect(body).to.contain('Alpha-selected').and.contain('Beta-selected');
      expect(body).not.to.contain('untouched');
      expect(body.trim().split('\n')).to.have.length(3);
    });
    cy.request(`/api/pools/contacts/export?contact=${beta.id}:${alpha.selected}`).then(({ body }) => {
      expect(body.trim().split('\n')).to.have.length(1);
    });
    cy.request(`/api/customer-lists/${alpha.id}/pool-contacts/export?contact=0:${alpha.selected}`).then(({ body }) => {
      expect(body).to.contain('Alpha-selected').and.not.contain('untouched');
    });
    cy.request(`/api/pools/contacts/export?search=untouched&contact=${alpha.id}:${alpha.selected}`).then(({ body }) => {
      expect(body.trim().split('\n')).to.have.length(1);
    });
    cy.request({ url: '/api/pools/contacts/export?contact=bad', failOnStatusCode: false }).its('status').should('eq', 400);
  });

  it('shows the Chinese pool toolbar and sends selected membership IDs to export', () => {
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    cy.visit('/admin/pool');
    selectContact('Alpha-selected');
    selectContact('Beta-selected');
    cy.get('[data-cy=pool-contacts-table] .pool-toolbar').should('contain', '已选择 2');
    cy.get('[data-cy=btn-assign-pool-contacts]').should('contain', '分配到组织');
    cy.get('[data-cy=btn-delete-pool-contacts]').should('contain', '删除公海客户');
    cy.viewport(1280, 900);
    cy.screenshot('pool-bulk-selected-zh-CN');
    cy.intercept('GET', '/api/pools/contacts/export*').as('export');
    cy.get('[data-cy=btn-export-pool-contacts]').click();
    cy.get('.dialog').should('contain', '2');
    confirmDialog();
    cy.wait('@export').then(({ request, response }) => {
      expect(new URL(request.url).searchParams.getAll('contact')).to.have.members(pools.map((pool) => `${pool.id}:${pool.selected}`));
      expect(response.statusCode).to.eq(200);
      expect(response.body).not.to.contain('untouched');
    });
  });

  it('selects across pools and assigns each selection to its source pool allocation', () => {
    cy.visit('/admin/pool');
    selectContact('Alpha-selected');
    selectContact('Beta-selected');
    cy.get('[data-cy=pool-contacts-table] .pool-toolbar').should('contain', '2 selected');
    cy.get('[data-cy=btn-delete-pool-contacts]').should('be.visible');
    cy.get('[data-cy=btn-assign-pool-contacts]').click();
    cy.get('[data-cy=pool-assign-allocation]').should('have.length', 2);
    cy.intercept('POST', '/api/org-pool-allocations/members').as('assign');
    cy.get('[data-cy=btn-confirm-pool-assign]').click();
    cy.wait(['@assign', '@assign']).then((calls) => {
      expect(calls.map(({ request }) => request.body)).to.have.deep.members(pools.map((pool) => ({
        allocation_id: pool.allocationID, contact_id: pool.selected,
      })));
      calls.forEach(({ response }) => expect(response.statusCode).to.eq(200));
    });
    cy.get('[data-cy=btn-assign-pool-contacts]').should('not.exist');
    pools.forEach((pool) => {
      cy.request(`/api/customer-lists/${pool.allocationListID}/pool-contacts`).then(({ body }) => {
        expect(body.data.results.map((contact) => contact.id)).to.deep.equal([pool.selected]);
      });
    });
  });

  it('archives selected contacts across pools without changing other contacts', () => {
    cy.visit('/admin/pool');
    selectContact('Alpha-selected');
    selectContact('Beta-selected');
    cy.intercept('DELETE', '/api/customer-lists/*/pool-contacts/*/email').as('archive');
    cy.get('[data-cy=btn-clear-pool-emails]').click();
    confirmDialog();
    cy.wait(['@archive', '@archive']).then((calls) => {
      expect(calls.map(({ request }) => new URL(request.url).pathname)).to.have.members(pools.map((pool) => (
        `/api/customer-lists/${pool.id}/pool-contacts/${pool.selected}/email`
      )));
      calls.forEach(({ response }) => expect(response.statusCode).to.eq(200));
    });
    cy.get('[data-cy=btn-clear-pool-emails]').should('not.exist');
    pools.forEach((pool) => {
      cy.request(`/api/customer-lists/${pool.id}/pool-contacts`).then(({ body }) => {
        expect(body.data.results.find((contact) => contact.id === pool.selected).status).to.eq('archived');
        expect(body.data.results.find((contact) => contact.id === pool.untouched).email).not.to.eq('');
      });
    });
  });

  it('deletes selected contacts after confirmation and preserves the others', () => {
    cy.visit('/admin/pool');
    selectContact('Alpha-selected');
    selectContact('Beta-selected');
    cy.intercept('DELETE', '/api/customer-lists/*/pool-contacts/*').as('delete');
    cy.get('[data-cy=btn-delete-pool-contacts]').click();
    cy.get('.dialog').should('contain', '2');
    confirmDialog();
    cy.wait(['@delete', '@delete']).then((calls) => {
      calls.forEach(({ response }) => expect(response.statusCode).to.eq(200));
    });
    cy.get('[data-cy=btn-delete-pool-contacts]').should('not.exist');
    cy.get('[data-cy=pool-contacts-table]').should('not.contain', 'Alpha-selected').and('not.contain', 'Beta-selected');
    cy.get('[data-cy=pool-contacts-table]').should('contain', 'Alpha-untouched').and('contain', 'Beta-untouched');
    cy.screenshot('pool-bulk-after-delete');
  });

  it('removes and restores aggregate selections only through their organization allocations', () => {
    pools.forEach((pool) => {
      cy.request('POST', '/api/org-pool-allocations/members', { allocation_id: pool.allocationID, contact_id: pool.untouched });
    });
    // Model an organization manager in the UI while exercising real endpoints
    // in this isolated admin-owned fixture. Backend authorization is unchanged.
    cy.intercept('GET', '/api/profile', (req) => req.continue((res) => {
      res.body.data.user_role.id = 2;
      res.body.data.user_role.permissions = ['pools:get', 'pools:manage', 'pools:export'];
    }));
    cy.setCookie('listmonk_workspace_organization_id', String(organizationID));
    cy.visit('/admin/pool', {
      onBeforeLoad(win) {
        win.localStorage.setItem('listmonk.workspace.organizationId', String(organizationID));
      },
    });
    selectContact('Alpha-untouched');
    selectContact('Beta-untouched');
    cy.get('[data-cy=btn-delete-pool-contacts]').should('not.exist');
    cy.get('[data-cy=btn-remove-pool-contacts]').click();
    cy.get('[data-cy=pool-removal-reason]').type('Bulk removal QA');
    cy.intercept('DELETE', '/api/org-pool-allocations/members').as('remove');
    cy.get('[data-cy=btn-confirm-pool-removal]').click();
    cy.wait(['@remove', '@remove']).then((calls) => {
      expect(calls.map(({ request }) => request.body)).to.have.deep.members(pools.map((pool) => ({
        allocation_id: pool.allocationID, contact_id: pool.untouched, reason: 'Bulk removal QA',
      })));
      calls.forEach(({ response }) => expect(response.statusCode).to.eq(200));
    });
    cy.get('[data-cy=pool-status-filter]').select('removed');
    selectContact('Alpha-untouched');
    selectContact('Beta-untouched');
    cy.intercept('PUT', '/api/pools/allocations/members').as('restore');
    cy.get('[data-cy=btn-restore-pool-contacts]').click();
    cy.wait(['@restore', '@restore']).then((calls) => {
      expect(calls.map(({ request }) => request.body)).to.have.deep.members(pools.map((pool) => ({
        allocation_id: pool.allocationID, contact_id: pool.untouched,
      })));
      calls.forEach(({ response }) => expect(response.statusCode).to.eq(200));
    });
    cy.get('[data-cy=btn-restore-pool-contacts]').should('not.exist');
  });

  [true, false].forEach((exportAllowed) => {
    it(`respects pool read/export permissions (export=${exportAllowed})`, () => {
      cy.intercept('GET', '/api/profile', (req) => req.continue((res) => {
        res.body.data.user_role.id = 2;
        res.body.data.user_role.permissions = ['pools:get', ...(exportAllowed ? ['pools:export'] : [])];
      }));
      cy.visit('/admin/pool');
      cy.get('[data-cy=pool-contacts-table] tbody').should('contain', 'Alpha-untouched');
      if (exportAllowed) {
        selectContact('Alpha-untouched');
        cy.get('[data-cy=btn-export-pool-contacts]').should('be.visible');
        cy.get('.pool-toolbar').should('contain', '1 selected');
      } else {
        cy.get('[data-cy=pool-contacts-table] .checkbox-cell').should('not.exist');
        cy.get('[data-cy=btn-export-pool-contacts]').should('not.exist');
      }
      cy.get('[data-cy=btn-assign-pool-contacts], [data-cy=btn-clear-pool-emails], [data-cy=btn-delete-pool-contacts]').should('not.exist');
    });
  });
});
