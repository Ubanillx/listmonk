/* eslint-env mocha */
/* global cy, expect, Cypress */

const defaults = () => ({
  enabled: false, types: [1, 3, 6, 8, 9], schedule: 'daily', daily_time: '02:00', interval_hours: 24, lookback_days: 7, excluded_emails: [],
});

describe('Platform delivery provider automation', () => {
  before(() => { cy.resetDB(); });
  beforeEach(() => {
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'zh-CN'; }));
  });

  it('keeps schedules off and preserves draft conditions without manual save steps', () => {
    cy.loginAndVisit('/admin/delivery-provider'); cy.get('[data-cy=provider-tab-automation]').click();
    cy.get('.automation-config').should('not.exist');
    cy.get('[data-cy=provider-tab-automation] .mdi-shield-check-outline').should('be.visible');
    cy.get('[data-cy=automation-private]').within(() => {
      cy.contains('button', '手动处理').click();
      cy.contains('button', '查找待拉黑客户').should('be.enabled');
      cy.contains('button', '保存规则').should('not.exist'); cy.contains('button', '预览').should('not.exist');
      cy.get('input[type=number]').clear().type('9');
      cy.contains('button', '收起设置').click(); cy.contains('button', '手动处理').click();
      cy.get('input[type=number]').should('have.value', '9');
      cy.contains('label', '启用定时拉黑').click();
      cy.get('.automation-schedule-options').should('be.visible');
      cy.get('input[type=time]').clear().type('03:30');
      cy.contains('button', '保存定时设置').should('be.enabled');
      cy.contains('label', '启用定时拉黑').click();
      cy.get('.automation-config').should('not.exist');
      cy.contains('label', '启用定时拉黑').click(); cy.get('input[type=time]').should('have.value', '03:30');
      cy.contains('label', '启用定时拉黑').click();
    });
    cy.get('[data-cy=automation-pool] .automation-config').should('not.exist');
    cy.request('/api/delivery-provider/automation').then(({ body }) => {
      body.data.scopes.forEach(({ rule }) => expect(rule.config.enabled).to.equal(false));
      const { rule } = body.data.scopes[0];
      cy.request({ method: 'PUT', url: '/api/delivery-provider/automation/private', failOnStatusCode: false,
        body: { version: rule.version, config: { ...rule.config, enabled: true } } }).its('status').should('eq', 409);
    });
    cy.reload(); cy.get('.automation-config').should('not.exist');
  });

  it('automatically checks current manual conditions, cancels without writes, then confirms and executes', () => {
    const rules = ['private', 'pool'].map((scope) => ({ rule: { scope, version: 1, config: defaults() }, runs: [] }));
    const run = { id: 12, trigger: 'preview', status: 'preview', date_start: '2026-10-28', date_end: '2026-11-03',
      counts: { addresses: 2, to_block: 1, unmatched: 1 }, created_at: '2026-11-03T04:00:00+08:00' };
    const item = { target_id: 4, name: 'Test customer', email: 'customer@example.test', outcome: 'to_block', evidence: { types: [1, 9], count: 2 } };
    let checked; let saves = 0; let runs = 0;
    cy.intercept('GET', '/api/delivery-provider/automation', (req) => req.reply({ data: { scopes: rules } }));
    cy.intercept('PUT', '/api/delivery-provider/automation/*', (req) => { saves += 1; req.reply({ statusCode: 500 }); });
    cy.intercept('POST', '/api/delivery-provider/automation/private/preview', (req) => {
      checked = req.body.config; req.reply({ data: { run, items: [item] } });
    }).as('check');
    cy.intercept('GET', '/api/delivery-provider/automation/private/runs/12*', { data: [item] });
    cy.intercept('POST', '/api/delivery-provider/automation/private/run', (req) => {
      runs += 1; expect(req.body).to.deep.equal({ version: 1, preview_id: 12, config: checked });
      expect(req.body.config.lookback_days).to.equal(9);
      expect(req.body.config.excluded_emails).to.deep.equal(['except@example.test']);
      rules[0].runs = [{ ...run, status: 'success', trigger: 'manual' }];
      req.reply({ data: { run: rules[0].runs[0], items: [{ ...item, outcome: 'blocked' }] } });
    }).as('run');
    cy.loginAndVisit('/admin/delivery-provider'); cy.get('[data-cy=provider-tab-automation]').click();
    cy.get('[data-cy=automation-private]').within(() => {
      cy.contains('button', '手动处理').click(); cy.get('input[type=number]').clear().type('9'); cy.contains('button', '查找待拉黑客户').click();
    });
    cy.wait('@check');
    cy.get('[data-cy=automation-private] input[type=number]').should('be.disabled');
    cy.viewport(1440, 900); cy.screenshot('automation-confirm-current-conditions', { capture: 'viewport' });
    cy.get('.dialog').should('contain', '1 个私域客户').contains('button', '取消').click();
    cy.get('.automation-history').should('contain', '未匹配邮箱 1');
    cy.then(() => { expect(runs).to.equal(0); expect(saves).to.equal(0); });
    cy.get('[data-cy=automation-private]').within(() => {
      cy.get('input[type=number]').should('have.value', '9');
      cy.get('.automation-exclusions > summary').click(); cy.get('textarea').type('except@example.test');
      cy.get('.automation-preview').should('not.exist'); cy.contains('button', '查找待拉黑客户').click();
    });
    cy.wait('@check'); cy.get('.dialog').contains('button', '确认拉黑').click(); cy.wait('@run');
    cy.get('@check.all').should('have.length', 2);
    cy.get('[data-cy=automation-private]').should('contain', '已拉黑 1 个客户');
    cy.get('.automation-history').should('contain', '已拉黑客户 1');
    cy.get('.automation-records select').select('pool'); cy.get('.automation-history').should('contain', '暂无处理记录');
    cy.get('.automation-records select').select('private');
    cy.then(() => { expect(saves).to.equal(0); expect(rules[0].rule.config.enabled).to.equal(false); });
    cy.viewport(1440, 900); cy.screenshot('automation-manual-complete', { capture: 'viewport' });
    cy.viewport(390, 844); cy.get('.automation-history').contains('button', '查看明细').click();
    cy.get('.automation-detail').should('be.visible').and('contain', 'customer@example.test');
    cy.get('.automation-detail').contains('button', '关闭').click();
    cy.document().then((doc) => { expect(doc.documentElement.scrollWidth).to.be.at.most(390); });
  });

  it('automatically checks before saving a schedule and preserves inputs after failed checks or saves', () => {
    const rules = ['private', 'pool'].map((scope) => ({ rule: { scope, version: 1, config: defaults() }, runs: [] }));
    let checked; let failCheck = true; let failSave = true;
    cy.intercept('GET', '/api/delivery-provider/automation', (req) => req.reply({ data: { scopes: rules } }));
    cy.intercept('POST', '/api/delivery-provider/automation/private/preview', (req) => {
      checked = req.body.config;
      if (failCheck) req.reply({ statusCode: 502, body: { message: 'Provider unavailable' } });
      else req.reply({ data: { run: { id: 51, counts: { to_block: 1 } } } });
    }).as('check');
    cy.intercept('PUT', '/api/delivery-provider/automation/private', (req) => {
      expect(req.body).to.deep.equal({ version: 1, config: checked, preview_id: 51 });
      expect(req.body.config.enabled).to.equal(true);
      if (failSave) req.reply({ statusCode: 409, body: { message: 'Rule changed' } });
      else { rules[0].rule = { ...rules[0].rule, version: 2, config: req.body.config }; req.reply({ data: rules[0].rule }); }
    }).as('save');
    cy.loginAndVisit('/admin/delivery-provider'); cy.get('[data-cy=provider-tab-automation]').click();
    cy.get('[data-cy=automation-private]').within(() => {
      cy.contains('label', '启用定时拉黑').click(); cy.get('input[type=time]').clear().type('03:30');
      cy.get('.automation-exclusions > summary').click(); cy.get('textarea').type('keep@example.test');
      cy.contains('button', '保存定时设置').click(); cy.wait('@check');
      cy.get('[role=alert]').should('contain', 'Provider unavailable'); cy.get('textarea').should('have.value', 'keep@example.test');
      cy.then(() => { failCheck = false; });
      cy.contains('button', '保存定时设置').click(); cy.wait('@check'); cy.wait('@save');
      cy.get('[role=alert]').should('contain', 'Rule changed'); cy.get('input[type=time]').should('have.value', '03:30');
      cy.then(() => { failSave = false; });
      cy.contains('button', '保存定时设置').click(); cy.wait('@check'); cy.wait('@save');
      cy.contains('定时设置已保存').should('be.visible');
      cy.get('input[type=time]').clear().type('04:00');
      cy.contains('定时设置已保存').should('not.exist');
    });
    cy.reload(); cy.get('[data-cy=automation-private] input[type=time]').should('have.value', '03:30');
    cy.get('[data-cy=automation-private] textarea').should('have.value', 'keep@example.test');
    cy.get('[data-cy=automation-pool] .automation-config').should('not.exist');
    cy.intercept('GET', '/api/profile', { data: { id: 77, user_role: { id: 77, permissions: ['delivery_provider:get', 'workspaces:personal'] }, customer_list_roles: [] } });
    cy.reload(); cy.get('[data-cy=provider-tab-automation]').should('not.exist');
  });

  it('explains zero matches and rechecks automatically after a failed execution', () => {
    const rules = ['private', 'pool'].map((scope) => ({ rule: { scope, version: 1, config: defaults() }, runs: [] }));
    let checks = 0; let failRun = true;
    cy.intercept('GET', '/api/delivery-provider/automation', { data: { scopes: rules } });
    cy.intercept('POST', '/api/delivery-provider/automation/pool/preview', (req) => {
      checks += 1;
      req.reply({ data: { run: { id: checks, date_start: '2026-10-29', date_end: '2026-11-04',
        counts: { addresses: 2, unmatched: checks === 1 ? 2 : 1, to_block: checks === 1 ? 0 : 1 } } } });
    }).as('check');
    cy.intercept('POST', '/api/delivery-provider/automation/pool/run', (req) => {
      expect(req.body.preview_id).to.equal(checks);
      if (failRun) req.reply({ statusCode: 409, body: { message: 'Check expired. Retry.' } });
      else req.reply({ data: { run: { id: 90, status: 'success', trigger: 'manual', counts: { to_block: 1 } } } });
    }).as('run');
    cy.loginAndVisit('/admin/delivery-provider'); cy.get('[data-cy=provider-tab-automation]').click();
    cy.get('[data-cy=automation-pool]').within(() => {
      cy.contains('button', '手动处理').click(); cy.contains('button', '查找待拉黑客户').click(); cy.wait('@check');
      cy.get('.automation-preview').should('contain', '当前条件下没有待拉黑客户').and('contain', '未匹配邮箱');
      cy.get('.dialog').should('not.exist');
      cy.contains('button', '查找待拉黑客户').should('be.enabled').click(); cy.wait('@check');
    });
    cy.get('.dialog').should('contain', '1 个公海客户').contains('button', '确认拉黑').click(); cy.wait('@run');
    cy.get('[data-cy=automation-pool]').within(() => {
      cy.get('[role=alert]').should('contain', 'Check expired'); cy.then(() => { failRun = false; });
      cy.contains('button', '查找待拉黑客户').click(); cy.wait('@check');
    });
    cy.get('.dialog').contains('button', '确认拉黑').click(); cy.wait('@run');
    cy.get('[data-cy=automation-pool]').should('contain', '已拉黑 1 个客户'); cy.then(() => expect(checks).to.equal(3));
  });

  ['tab', 'route', 'tab-and-return'].forEach((navigation) => {
    it(`discards a delayed check after leaving the automation ${navigation} and permits a fresh check`, () => {
      const rules = ['private', 'pool'].map((scope) => ({ rule: { scope, version: 1, config: defaults() }, runs: [] }));
      const run = { id: 61, trigger: 'preview', status: 'preview', counts: { to_block: 1 }, created_at: '2026-11-04T10:00:00+08:00' };
      let release; let checks = 0; let writes = 0;
      cy.intercept('GET', '/api/delivery-provider/automation', { data: { scopes: rules } });
      cy.intercept('PUT', '/api/delivery-provider/automation/*', (req) => { writes += 1; req.reply({ statusCode: 500 }); });
      cy.intercept('POST', '/api/delivery-provider/automation/private/run', (req) => { writes += 1; req.reply({ statusCode: 500 }); });
      cy.intercept('POST', '/api/delivery-provider/automation/private/preview', (req) => {
        checks += 1;
        if (checks === 1) return new Cypress.Promise((resolve) => { release = () => { req.reply({ data: { run } }); resolve(); }; });
        return req.reply({ data: { run: { ...run, id: 62 } } });
      }).as('delayedCheck');
      cy.loginAndVisit('/admin/delivery-provider'); cy.get('[data-cy=provider-tab-automation]').click();
      cy.get('[data-cy=automation-private]').within(() => {
        cy.contains('button', '手动处理').click(); cy.get('input[type=number]').clear().type('9');
        cy.contains('button', '查找待拉黑客户').click();
      });
      cy.wrap(null).should(() => expect(release).to.be.a('function'));
      if (navigation === 'route') cy.get('.logo a').click();
      else cy.get('[data-cy=provider-tab-overview]').click();
      cy.get('[data-cy=automation-private]').should('not.exist');
      if (navigation === 'tab-and-return') cy.get('[data-cy=provider-tab-automation]').click();
      cy.then(() => release()); cy.wait('@delayedCheck');
      cy.get('.dialog').should('not.exist');
      if (navigation === 'route') cy.get('[data-cy=delivery-provider]').click();
      cy.get('[data-cy=provider-tab-automation]').click();
      cy.get('[data-cy=automation-private]').within(() => {
        if (navigation === 'route') cy.contains('button', '手动处理').click();
        cy.contains('button', '查找待拉黑客户').should('be.enabled').click();
      });
      cy.wait('@delayedCheck'); cy.get('.dialog').contains('button', '取消').click();
      cy.then(() => expect(writes).to.equal(0));
    });
  });

  it('closes all owned confirmations on navigation without executing either scope', () => {
    const rules = ['private', 'pool'].map((scope) => ({ rule: { scope, version: 1, config: defaults() }, runs: [] }));
    let writes = 0; const releases = [];
    cy.intercept('GET', '/api/delivery-provider/automation', { data: { scopes: rules } });
    cy.intercept('POST', '/api/delivery-provider/automation/*/preview', (req) => new Cypress.Promise((resolve) => {
      releases.push(() => { req.reply({ data: { run: { id: 63, counts: { to_block: 1 } } } }); resolve(); });
    }));
    cy.intercept('POST', '/api/delivery-provider/automation/*/run', (req) => { writes += 1; req.reply({ statusCode: 500 }); });
    cy.loginAndVisit('/admin/delivery-provider'); cy.get('[data-cy=provider-tab-automation]').click();
    ['private', 'pool'].forEach((scope) => {
      cy.get(`[data-cy=automation-${scope}]`).within(() => {
        cy.contains('button', '手动处理').click(); cy.contains('button', '查找待拉黑客户').click();
      });
    });
    cy.wrap(null).should(() => expect(releases).to.have.length(2)); cy.then(() => releases.forEach((release) => release()));
    cy.get('.dialog').should('have.length', 2);
    // A browser history or router change can leave the page while the dialog is open.
    cy.window().then((win) => win.document.querySelector('#app').__vue__.$router.push('/'));
    cy.get('[data-cy=automation-private]').should('not.exist'); cy.get('.dialog').should('not.exist');
    cy.then(() => expect(writes).to.equal(0));
  });

  it('uses saved schedule fields for manual checks while retaining invalid schedule drafts independently per scope', () => {
    const rules = ['private', 'pool'].map((scope) => ({ rule: { scope, version: 1, config: defaults() }, runs: [] }));
    let writes = 0;
    cy.intercept('GET', '/api/delivery-provider/automation', { data: { scopes: rules } });
    cy.intercept('PUT', '/api/delivery-provider/automation/*', (req) => { writes += 1; req.reply({ statusCode: 500 }); });
    ['private', 'pool'].forEach((scope) => {
      cy.intercept('POST', `/api/delivery-provider/automation/${scope}/preview`, (req) => {
        expect(req.body.config).to.deep.equal({ ...defaults(), types: [1, 3, 6, 8, 9, 7], lookback_days: 9, excluded_emails: ['except@example.test'] });
        req.reply({ data: { run: { id: 64, counts: { to_block: 0 } } } });
      }).as(`${scope}Manual`);
    });
    cy.loginAndVisit('/admin/delivery-provider'); cy.get('[data-cy=provider-tab-automation]').click();
    ['private', 'pool'].forEach((scope) => {
      cy.get(`[data-cy=automation-${scope}]`).within(() => {
        cy.contains('label', '启用定时拉黑').click();
        if (scope === 'private') cy.get('input[type=time]').clear();
        else {
          cy.get('.automation-schedule-options select').select('interval');
          cy.get('#automation-interval-pool').clear().type('999').should('have.value', '999');
        }
        cy.contains('label', '投诉地址').click(); cy.get('.automation-lookback input').clear().type('9');
        cy.get('.automation-exclusions > summary').click(); cy.get('textarea').type('except@example.test');
        cy.contains('label', '启用定时拉黑').click(); cy.contains('button', '手动处理').click();
        cy.contains('button', '查找待拉黑客户').click(); cy.wait(`@${scope}Manual`);
        cy.get('.automation-preview').should('contain', '当前条件下没有待拉黑客户');
        cy.contains('label', '启用定时拉黑').click();
        if (scope === 'private') cy.get('input[type=time]').should('have.value', '');
        else cy.get('#automation-interval-pool').should('have.value', '999');
        cy.contains('label', '启用定时拉黑').click();
      });
    });
    cy.then(() => expect(writes).to.equal(0));
  });

  it('refreshes failed check and execution history without losing inputs or replacing the original error when history fails', () => {
    const rules = ['private', 'pool'].map((scope) => ({ rule: { scope, version: 1, config: defaults() }, runs: [] }));
    const run = { id: 71, trigger: 'preview', status: 'failed', counts: { to_block: 0 }, error: 'Check failed in provider', created_at: '2026-11-04T10:00:00+08:00' };
    let checks = 0; let failHistory = false;
    cy.intercept('GET', '/api/delivery-provider/automation', (req) => {
      if (failHistory) { failHistory = false; req.reply({ statusCode: 503, body: { message: 'History unavailable' } }); }
      else req.reply({ data: { scopes: rules } });
    }).as('history');
    cy.intercept('POST', '/api/delivery-provider/automation/private/preview', (req) => {
      checks += 1;
      if (checks === 1) { rules[0].runs = [run]; req.reply({ statusCode: 502, body: { message: 'Check request failed' } }); }
      else req.reply({ data: { run: { ...run, id: 72, error: '', status: 'preview', counts: { to_block: 1 } } } });
    }).as('checkFailure');
    cy.intercept('POST', '/api/delivery-provider/automation/private/run', (req) => {
      rules[0].runs = [{ ...run, id: 73, trigger: 'manual', error: 'Execution failed in provider' }, run];
      failHistory = true; req.reply({ statusCode: 502, body: { message: 'Execution request failed' } });
    }).as('runFailure');
    cy.loginAndVisit('/admin/delivery-provider'); cy.get('[data-cy=provider-tab-automation]').click();
    cy.get('[data-cy=automation-private]').within(() => {
      cy.contains('button', '手动处理').click(); cy.get('.automation-lookback input').clear().type('9');
      cy.get('.automation-exclusions > summary').click(); cy.get('textarea').type('keep@example.test');
      cy.contains('button', '查找待拉黑客户').click(); cy.wait('@checkFailure');
      cy.get('[role=alert]').should('contain', 'Check request failed');
    });
    cy.get('.automation-history').should('contain', 'Check failed in provider');
    cy.get('[data-cy=automation-private]').contains('button', '查找待拉黑客户').click(); cy.wait('@checkFailure');
    cy.get('.dialog').contains('button', '确认拉黑').click(); cy.wait('@runFailure');
    cy.get('[data-cy=automation-private]').within(() => {
      cy.get('[role=alert]').should('contain', 'Execution request failed');
      cy.get('.automation-history-error').should('be.visible').contains('button', '重试').click();
      cy.get('.automation-lookback input').should('have.value', '9'); cy.get('textarea').should('have.value', 'keep@example.test');
      cy.get('[role=alert]').should('contain', 'Execution request failed');
      cy.get('.automation-history-error').should('not.exist');
    });
    cy.get('.automation-history').should('contain', 'Execution failed in provider');
    cy.get('.automation-heading-actions input[type=checkbox]').should('not.be.checked');
  });

  it('keeps long detail lists bounded with a full-width footer and reachable controls', () => {
    const counts = { addresses: 53, to_block: 2, already: 0, excluded: 0, unmatched: 52, skipped: 0 };
    const run = { id: 31, trigger: 'preview', status: 'preview', counts, date_start: '2026-10-29', date_end: '2026-11-04', created_at: '2026-11-04T09:40:00+08:00' };
    const rows = Array.from({ length: 54 }, (_, index) => ({ target_id: 0, name: '', email: `long-email-address-${index}@example.test`,
      outcome: 'unmatched', evidence: { types: [1, 3], count: 2 } }));
    rows[0] = { ...rows[0], target_id: 1, name: 'Customer A', outcome: 'to_block' };
    rows[1] = { ...rows[1], email: rows[0].email, target_id: 2, name: 'Customer B', outcome: 'to_block' };
    cy.intercept('GET', '/api/delivery-provider/automation', { data: { scopes: [
      { rule: { scope: 'private', version: 1, config: defaults() }, runs: [run] },
      { rule: { scope: 'pool', version: 1, config: defaults() }, runs: [] },
    ] } });
    let failedSecondPage = false;
    cy.intercept('GET', '/api/delivery-provider/automation/private/runs/31*', (req) => {
      const page = Number(req.query.page || 1);
      if (page === 2 && !failedSecondPage) {
        failedSecondPage = true;
        req.reply({ statusCode: 503, body: { message: 'Detail page unavailable' } });
      } else req.reply({ data: rows.slice((page - 1) * 50, page * 50) });
    }).as('details');
    cy.viewport(1440, 900);
    cy.loginAndVisit('/admin/delivery-provider');
    cy.get('[data-cy=provider-tab-automation]').click();
    cy.get('.automation-history').contains('button', '查看明细').as('detailTrigger').click();
    cy.wait('@details');
    cy.get('.automation-detail').should('be.visible').within(() => {
      cy.get('.modal-card-foot').should('contain', '共 54 条');
      cy.get('.modal-card-foot').should(($footer) => {
        const footer = $footer[0].getBoundingClientRect();
        const card = $footer[0].closest('.modal-card').getBoundingClientRect();
        expect(Math.abs(footer.width - card.width), 'footer spans the dialog').to.be.lessThan(3);
        expect(card.height, 'dialog leaves space around it').to.be.at.most(836);
      });
      cy.get('tbody tr').should('have.length', 20);
      cy.get('.modal-card-body').scrollTo('bottom');
      cy.get('.modal-card-head button').should('be.visible');
      cy.get('.modal-card-foot').contains('button', '关闭').should('be.visible');
      cy.get('[data-cy=page-jump]').clear().type('3');
      cy.get('[data-cy=page-jump-submit]').click();
      cy.get('[role=alert]').should('contain', 'Detail page unavailable');
      cy.get('tbody tr').should('not.exist');
      cy.contains('button', '重试').click();
      cy.get('tbody tr').should('have.length', 14).and('contain', 'long-email-address-40@example.test');
      cy.get('.modal-card-body').should('have.prop', 'scrollTop', 0);
      cy.get('[data-cy=page-size]').select('50');
      cy.get('tbody tr').should('have.length', 50).and('contain', 'long-email-address-0@example.test');
      cy.get('[data-cy=page-size]').select('100');
      cy.get('tbody tr').should('have.length', 54).and('contain', 'long-email-address-53@example.test');
    });
    cy.screenshot('automation-detail-desktop', { capture: 'viewport' });
    cy.viewport(390, 844);
    cy.get('.automation-detail').should(($card) => {
      const rect = $card[0].getBoundingClientRect();
      const modal = $card[0].closest('.modal').getBoundingClientRect();
      expect(Math.abs((rect.left - modal.left) - (modal.right - rect.right)), 'dialog is centered in the visible area').to.be.lessThan(3);
    }).within(() => {
      cy.get('.modal-card-head button').should('be.visible');
      cy.get('.modal-card-foot').contains('button', '关闭').should('be.visible');
      cy.get('.modal-card-body').should(($body) => { expect($body[0].scrollWidth).to.be.at.most($body[0].clientWidth + 1); });
      cy.get('.modal-card-foot').contains('button', '关闭').click();
    });
    cy.get('@detailTrigger').should('be.focused');
  });

  it('keeps stopping a saved schedule available after collapsing its form and after a failed save', () => {
    const rules = ['private', 'pool'].map((scope) => ({ rule: { scope, version: 1, config: { ...defaults(), enabled: scope === 'private' } }, runs: [] }));
    cy.intercept('GET', '/api/delivery-provider/automation', (req) => req.reply({ data: { scopes: rules } }));
    let saves = 0;
    cy.intercept('PUT', '/api/delivery-provider/automation/private', (req) => {
      expect(req.body.config).to.deep.equal(defaults());
      saves += 1;
      if (saves === 1) req.reply({ statusCode: 503, body: { message: 'Save unavailable' } });
      else {
        rules[0].rule = { ...rules[0].rule, version: 2, config: defaults() };
        req.reply({ data: rules[0].rule });
      }
    }).as('disable');
    cy.loginAndVisit('/admin/delivery-provider?tab=automation');
    cy.get('[data-cy=provider-tab-automation]').click();
    cy.get('[data-cy=automation-private]').within(() => {
      cy.get('.automation-config').should('be.visible');
      // A malformed, unsaved draft must not prevent stopping the saved schedule.
      cy.get('input[type=number]').clear().type('99');
      cy.contains('label', '启用定时拉黑').click();
      cy.get('.automation-config').should('not.exist');
      cy.get('.automation-heading').should('contain', '保存后停用');
      cy.contains('button', '保存停用').click(); cy.wait('@disable');
      cy.get('[role=alert]').should('contain', 'Save unavailable');
      cy.contains('button', '保存停用').should('be.visible').click(); cy.wait('@disable');
      cy.contains('定时设置已保存').should('be.visible');
      cy.contains('button', '保存停用').should('not.exist');
    });
    cy.reload();
    cy.get('.automation-config').should('not.exist');
    cy.get('.automation-heading-actions input[type=checkbox]').should('not.be.checked');
    cy.viewport(390, 844);
    cy.document().then((doc) => { expect(doc.documentElement.scrollWidth).to.be.at.most(390); });
    cy.screenshot('automation-default-mobile', { capture: 'viewport' });
  });
});
