/* eslint-env mocha */
/* global cy, expect */

const list = (id, name, type, extra = {}) => ({
  id,
  name,
  type,
  optin: 'single',
  status: 'active',
  tags: [],
  visibility: type === 'pool' ? 'global' : 'organization',
  owner_user_id: 1,
  owner_username: 'admin',
  customer_count: 3,
  customer_statuses: {},
  created_at: '2026-09-28T08:00:00Z',
  updated_at: '2026-09-28T08:00:00Z',
  ...extra,
});

const north = list(12, 'North allocation', 'org_pool_allocation', { pool_parent_id: 10, organization_id: 1, organization_name: 'North' });
const south = list(13, 'South allocation', 'org_pool_allocation', { pool_parent_id: 10, organization_id: 2, organization_name: 'South' });
const rows = [north, south, list(10, 'Primary pool', 'pool'), list(20, 'Empty pool', 'pool'),
  list(30, 'Visible allocation', 'org_pool_allocation', { pool_parent_id: 999, organization_id: 3, organization_name: 'West' })];

function mockLists(results = rows) {
  cy.intercept('GET', '/api/customer-lists*', (req) => {
    if (req.query.type_group === 'pool') {
      expect(req.query.per_page).to.equal('all');
      req.reply({
        data: {
          results, total: results.length, page: 1, per_page: results.length,
        },
      });
    } else req.continue();
  }).as('poolLists');
}

describe('Public pool list hierarchy', () => {
  beforeEach(() => {
    cy.resetDB();
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
  });

  it('shows unsubscribe counts on pools and allocations and opens the matching contact filter', () => {
    cy.viewport(1280, 720);
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
    mockLists([
      { ...north, customer_statuses: { unsubscribed: 1 } },
      list(10, 'Primary pool', 'pool', { customer_statuses: { unsubscribed: 2 } }),
      list(20, 'Empty pool', 'pool'),
    ]);
    cy.loginAndVisit('/admin/pool-lists');
    cy.contains('tbody tr', 'Primary pool').find('.status-item.unsubscribed')
      .should('contain', '退订').and('contain', '2')
      .and('have.attr', 'href', '/admin/pool-lists/10/contacts?pool_status=unsubscribed');
    cy.contains('tbody tr', 'North allocation').find('.status-item.unsubscribed')
      .should('contain', '1').and('have.attr', 'href', '/admin/pool-lists/12/contacts?pool_status=unsubscribed');
    cy.contains('tbody tr', 'Empty pool').find('.status-item.is-empty')
      .should('contain', '退订').and('contain', '0')
      .and('have.attr', 'href', '/admin/pool-lists/20/contacts?pool_status=unsubscribed');
    cy.contains('tbody tr', 'Empty pool').find('.status-item.unsubscribed').should('not.exist');
    cy.get('tbody .fields.stats > .tag').should('not.exist');
    cy.screenshot('pool-list-unsubscribe-counts-desktop', { capture: 'viewport' });
    cy.viewport(390, 844);
    cy.contains('tbody tr', 'Primary pool').find('.status-item.unsubscribed').should('be.visible');
    cy.document().then((doc) => { expect(doc.documentElement.scrollWidth).to.be.at.most(390); });
    cy.contains('tbody tr', 'Primary pool').scrollIntoView();
    cy.screenshot('pool-list-unsubscribe-counts-mobile', { capture: 'viewport' });
    cy.viewport(1400, 950);
    cy.intercept('GET', '/api/customer-lists/10', { data: list(10, 'Primary pool', 'pool') });
    cy.intercept('GET', '/api/customer-lists/10/org-pool-allocations*', { data: [] });
    cy.intercept('GET', '/api/customer-lists/10/pool-contacts*', {
      data: {
        results: [], total: 0, per_page: 20, page: 1,
      },
    }).as('unsubscribedContacts');
    cy.contains('tbody tr', 'Primary pool').find('.status-item.unsubscribed').click();
    cy.wait('@unsubscribedContacts').its('request.query').should('include', { status: 'unsubscribed' });
    cy.get('[data-cy=pool-status-filter]').should('have.value', 'unsubscribed');
    cy.reload();
    cy.get('[data-cy=pool-status-filter]').should('have.value', 'unsubscribed');
  });

  it('groups allocations by real parent, filters with context and keeps selection safe', () => {
    mockLists();
    cy.loginAndVisit('/admin/pool-lists');
    cy.get('tbody tr').should('have.length', 5);
    cy.get('tbody tr').eq(0).should('contain', 'Primary pool');
    cy.get('tbody tr').eq(1).should('contain', 'North allocation').and('have.class', 'pool-tree-child');
    cy.get('tbody tr').eq(2).should('contain', 'South allocation');
    cy.contains('tbody tr', 'Visible allocation').should('contain', 'Source pool is outside the current visible scope');
    cy.get('[data-cy=pool-list-result-count]').should('contain', '5 matching lists');
    cy.contains('tbody tr', 'Primary pool').find('.checkbox-cell .checkbox').click();
    cy.get('[data-cy=btn-delete-customer_lists]').should('be.visible');
    cy.get('[data-cy=pool-list-organization-filter]').select('1');
    cy.get('tbody tr').should('have.length', 2);
    cy.contains('tbody tr', 'Primary pool').should('contain', 'shown for context').find('input[type=checkbox]').should('be.disabled');
    cy.get('[data-cy=btn-delete-customer_lists]').should('not.exist');
    cy.get('[data-cy=pool-list-result-count]').should('contain', '1 matching lists');
    cy.get('[data-cy=pool-list-type-filter]').select('pool');
    cy.get('tbody').should('contain', 'No public pool lists match');
    cy.get('[data-cy=pool-list-reset]').click();
    cy.get('[data-cy=query]').type('North');
    cy.get('[data-cy=btn-query]').click();
    cy.get('tbody tr').should('have.length', 2);
    cy.contains('tbody tr', 'North allocation').should('exist');
    cy.get('[data-cy=pool-tree-toggle]').should('have.attr', 'aria-expanded', 'true').click();
    cy.get('tbody tr').should('have.length', 1);
    cy.get('[data-cy=pool-tree-toggle]').should('have.attr', 'aria-expanded', 'false');
    cy.get('[data-cy=pool-list-expand-all]').click();
    cy.get('tbody tr').should('have.length', 2);
    cy.get('[data-cy=pool-list-reset]').click();
    cy.get('[data-cy=pool-list-collapse-all]').click();
    cy.get('tbody tr').should('have.length', 3);
    cy.get('[data-cy=pool-list-expand-all]').click();
    cy.get('tbody tr').should('have.length', 5);
    cy.get('[data-cy=pool-list-type-filter]').select('pool');
    cy.get('tbody tr').should('have.length', 2).and('not.contain', 'allocation');
    cy.get('[data-cy=pool-list-reset]').click();
    cy.viewport(390, 844);
    cy.get('[data-cy=pool-list-organization-filter]').should('be.visible').select('2');
    cy.contains('tbody tr', 'South allocation').should('be.visible');
    cy.document().then((doc) => { expect(doc.documentElement.scrollWidth).to.be.at.most(390); });
  });

  it('paginates whole groups and resets to the first page when filtering', () => {
    const many = [];
    for (let id = 1; id <= 21; id += 1) {
      many.push(list(id, `Pool ${id}`, 'pool'));
      many.push(list(id + 100, `Allocation ${id}`, 'org_pool_allocation', {
        pool_parent_id: id, organization_id: id, organization_name: `Organization ${id}`,
      }));
    }
    mockLists(many);
    cy.loginAndVisit('/admin/pool-lists');
    cy.get('tbody tr').should('have.length', 40);
    cy.get('.pagination-next').click();
    cy.get('tbody tr').should('have.length', 2);
    cy.get('tbody tr').eq(0).should('contain', 'Pool 21');
    cy.get('tbody tr').eq(1).should('contain', 'Allocation 21');
    cy.get('select[data-cy=page-size]').select('10');
    cy.get('tbody tr').should('have.length', 20).first().should('contain', 'Pool 1');
    cy.get('[data-cy=page-jump]').clear().type('3{enter}');
    cy.get('tbody tr').should('have.length', 2).first().should('contain', 'Pool 21');
    cy.get('select[data-cy=page-size]').select('50');
    cy.get('tbody tr').should('have.length', 42);
    cy.get('[data-cy=pool-list-organization-filter]').select('1');
    cy.get('tbody tr').should('have.length', 2).and('contain', 'Allocation 1');
    cy.get('.pagination-link.is-current').should('contain', '1');
  });

  it('labels a public-pool import action correctly and opens the pool import flow', () => {
    let poolID;
    cy.loginAndVisit('/admin/pool-lists');
    cy.request('POST', '/api/customer-lists', { name: 'Primary pool', type: 'pool', optin: 'single' })
      .then(({ body }) => { poolID = body.data.id; });
    cy.visit('/admin/pool-lists');
    cy.contains('tbody tr', 'Primary pool').find('[data-cy=btn-import]')
      .should('have.attr', 'aria-label', 'Import public-pool customers').click();
    cy.then(() => cy.url().should('include', `/admin/customers/import?customer_list_id=${poolID}`));
    cy.get('[data-cy=import-tab-pool]').should('have.attr', 'aria-selected', 'true');
    cy.get('.import-list-setup').should('contain', 'Primary pool');
    cy.screenshot('pool-list-import-target', { capture: 'viewport' });
  });

  it('shows a retry after load failure and renders the recovered hierarchy', () => {
    cy.intercept(
      {
        method: 'GET', url: '/api/customer-lists*', query: { type_group: 'pool' }, times: 1,
      },
      { statusCode: 500, body: { message: 'Temporary test failure' } },
    );
    cy.loginAndVisit('/admin/pool-lists');
    cy.get('[role=alert]').should('contain', 'Unable to load public pool lists');
    mockLists();
    cy.get('[role=alert]').contains('button', 'Retry').click();
    cy.contains('tbody tr', 'Primary pool').should('be.visible');
    cy.get('[role=alert]').should('not.exist');
  });
});
