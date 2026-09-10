/* eslint-env mocha */
/* global cy, Cypress */

// Opt-in against the existing development stack; never resets or saves data.
// The gateway itself is stubbed, so the suite proves the settings workflow
// (address + key -> model list -> model -> test) without a live gateway.
describe('Reply AI gateway settings', function replyAISuite() {
  const baseUrlInput = 'input[name=reply_ai_base_url]';
  const apiKeyInput = 'input[name=reply_ai_api_key]';
  const models = [
    { id: 'gpt-4o-mini', owned_by: 'openai', chat_hint: true },
    { id: 'text-embedding-3-small', owned_by: 'openai', chat_hint: false },
  ];

  before(function enabled() {
    if (!Cypress.env('REPLY_AI_E2E')) this.skip();
    // A dev stack whose admin password is unknown can be exercised with an
    // existing session id: REPLY_AI_SESSION=<value of the `session` cookie>.
    if (Cypress.env('REPLY_AI_SESSION')) {
      cy.setCookie('session', Cypress.env('REPLY_AI_SESSION'));
    } else {
      cy.request('/admin/login').then((response) => {
        const nonce = /name="nonce" value="([^"]+)"/.exec(response.body)[1];
        cy.request({
          method: 'POST',
          url: '/admin/login',
          form: true,
          body: {
            username: Cypress.env('REPLY_AI_QA_USER') || 'root',
            password: Cypress.env('REPLY_AI_QA_PASSWORD') || 'Test@1234',
            nonce,
            next: '/admin',
          },
        });
      });
    }
    cy.request('/api/settings').its('status').should('eq', 200);
  });

  beforeEach(() => {
    cy.intercept('GET', '/api/settings', (req) => req.continue((res) => {
      res.body.data.reply_ai = {
        enabled: false,
        base_url: '',
        api_key: '',
        model: '',
        timeout: '15s',
        min_confidence: 0.98,
      };
    }));
    cy.visit('/admin/settings');
    cy.get('.b-tabs nav a').contains(/Reply AI|回信 AI/).click();
  });

  it('Requires the address and key before the model list can be loaded', () => {
    cy.get('[data-cy=reply-ai-load-models]').should('be.disabled');
    cy.get(baseUrlInput).clear().type('https://gateway.example.com/v1');
    cy.get('[data-cy=reply-ai-load-models]').should('be.disabled');
    cy.get(apiKeyInput).clear().type('sk-live-key');
    cy.get('[data-cy=reply-ai-load-models]').should('not.be.disabled');
    cy.get('[data-cy=reply-ai-test]').should('be.disabled');
  });

  it('Loads the gateway catalogue and selects a model from it', () => {
    let saves = 0;
    cy.intercept('PUT', '/api/settings', () => { saves += 1; });
    cy.intercept('POST', '/api/settings/reply-ai/models', (req) => {
      expect(req.body.base_url).to.equal('https://gateway.example.com/v1');
      expect(req.body.api_key).to.equal('sk-live-key');
      req.reply({ body: { data: { api_root: 'https://gateway.example.com/v1', models, chat_candidates: 1 } } });
    }).as('loadModels');

    cy.get(baseUrlInput).clear().type('https://gateway.example.com/v1');
    cy.get(apiKeyInput).clear().type('sk-live-key');
    cy.get('[data-cy=reply-ai-load-models]').click();
    cy.wait('@loadModels');
    cy.get('[data-cy=reply-ai-models-summary]').should('contain', '2');

    // Selecting from the catalogue fills the model field. Buefy forwards
    // data-cy onto the inner input of the autocomplete.
    cy.get('[data-cy=reply-ai-model]').click().type('gpt-4o');
    cy.contains('.autocomplete .dropdown-item', 'gpt-4o-mini').click();
    cy.get('[data-cy=reply-ai-model]').should('have.value', 'gpt-4o-mini');
    cy.get('[data-cy=reply-ai-test]').should('not.be.disabled');

    // Probing must never save the settings form.
    cy.then(() => expect(saves).to.equal(0));
  });

  it('Shows every test step and the model verdict', () => {
    cy.intercept('POST', '/api/settings/reply-ai/models', {
      body: { data: { api_root: 'https://gateway.example.com/v1', models, chat_candidates: 1 } },
    });
    cy.intercept('POST', '/api/settings/reply-ai/test', (req) => {
      expect(req.body.model).to.equal('gpt-4o-mini');
      expect(req.body.sample_text).to.equal('');
      req.reply({ body: { data: {
        status: 'success',
        api_root: 'https://gateway.example.com/v1',
        model: 'gpt-4o-mini',
        sample: 'Please stop sending me your newsletter and remove me from your mailing list immediately.',
        expected_intent: 'unsubscribe',
        matched: true,
        model_count: 2,
        latency_ms: 812,
        steps: [
          { name: 'config', status: 'success', detail: 'POST https://gateway.example.com/v1/chat/completions · timeout 15s' },
          { name: 'gateway', status: 'success', detail: 'GET https://gateway.example.com/v1/models · HTTP 200 · 2 models' },
          { name: 'model', status: 'success', detail: 'gpt-4o-mini is advertised by the gateway' },
          { name: 'completion', status: 'success', detail: 'intent=unsubscribe · confidence=0.99 · reason_code=explicit_unsubscribe' },
        ],
        decision: { intent: 'unsubscribe', confidence: 0.99, reason_code: 'explicit_unsubscribe' },
        response: '{"intent":"unsubscribe","confidence":0.99,"reason_code":"explicit_unsubscribe"}',
      } } });
    }).as('testModel');

    cy.get(baseUrlInput).clear().type('https://gateway.example.com/v1');
    cy.get(apiKeyInput).clear().type('sk-live-key');
    cy.get('[data-cy=reply-ai-load-models]').click();
    cy.get('[data-cy=reply-ai-model]').click().type('gpt-4o-mini');
    cy.contains('.autocomplete .dropdown-item', 'gpt-4o-mini').click();
    cy.get('[data-cy=reply-ai-test]').click();
    cy.wait('@testModel');

    cy.get('[data-cy=reply-ai-test-result] .notification').should('have.class', 'is-success');
    cy.get('[data-cy=reply-ai-test-result] p').should('have.length.at.least', 4);
    cy.get('[data-cy=reply-ai-test-result]').should('contain', '812').and('contain', 'explicit_unsubscribe');
    cy.get('[data-cy=reply-ai-verified]').should('exist');
  });

  it('Reports a rejected key as a warning on the gateway step', () => {
    cy.intercept('POST', '/api/settings/reply-ai/test', { body: { data: {
      status: 'failed',
      api_root: 'https://gateway.example.com/v1',
      model: 'gpt-4o-mini',
      sample: 'sample',
      latency_ms: 40,
      steps: [
        { name: 'config', status: 'success', detail: 'POST https://gateway.example.com/v1/chat/completions' },
        { name: 'gateway', status: 'warning', reason: 'auth_rejected', detail: 'HTTP 401: invalid token' },
        { name: 'model', status: 'warning', reason: 'model_list_unavailable', detail: 'no list' },
        { name: 'completion', status: 'failed', reason: 'auth_rejected', detail: 'HTTP 401: invalid token' },
      ],
    } } }).as('testModel');

    cy.get(baseUrlInput).clear().type('https://gateway.example.com/v1');
    cy.get(apiKeyInput).clear().type('sk-bad-key');
    cy.get('[data-cy=reply-ai-model]').clear().type('gpt-4o-mini');
    cy.get('[data-cy=reply-ai-test]').click();
    cy.wait('@testModel');

    cy.get('[data-cy=reply-ai-test-result] .notification').should('have.class', 'is-danger');
    cy.get('[data-cy=reply-ai-test-result]').should('contain', 'invalid token');
    cy.get('[data-cy=reply-ai-verified]').should('not.exist');
  });

  it('Offers the gateway root that answered', () => {
    cy.intercept('POST', '/api/settings/reply-ai/models', {
      body: { data: {
        api_root: 'https://gateway.example.com/v1',
        suggested_base_url: 'https://gateway.example.com/v1',
        models,
        chat_candidates: 1,
      } },
    });

    cy.get(baseUrlInput).clear().type('https://gateway.example.com');
    cy.get(apiKeyInput).clear().type('sk-live-key');
    cy.get('[data-cy=reply-ai-load-models]').click();
    cy.get('[data-cy=reply-ai-suggested-base]').should('contain', 'https://gateway.example.com/v1');
    cy.get('[data-cy=reply-ai-suggested-base] button').click();
    cy.get(baseUrlInput).should('have.value', 'https://gateway.example.com/v1');
  });
});
