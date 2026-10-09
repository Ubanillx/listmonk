/* eslint-env mocha */
/* global cy, expect */

const openCampaign = (name) => {
  cy.request('POST', '/api/campaigns', {
    name, subject: name, type: 'regular', content_type: 'richtext', body: '<p>Inline image regression</p>',
    customer_list_ids: [1], smtp_source: 'personal', smtp_rate_limit: 20, template_id: 1,
  }).then(({ body }) => cy.visit(`/admin/campaigns/${body.data.id}#content`));
  cy.get('.tox-tinymce').should('be.visible');
};

const imageData = (win) => {
  const canvas = win.document.createElement('canvas');
  canvas.width = 32;
  canvas.height = 16;
  canvas.getContext('2d').fillRect(0, 0, 32, 16);
  return canvas.toDataURL('image/png');
};

describe('Campaign inline image uploads', () => {
  beforeEach(() => {
    cy.intercept('GET', '/api/config', (req) => req.continue((res) => { res.body.data.lang = 'en'; }));
  });

  it('uploads dialog and pasted images, waits before saving, previews and sends remotely loaded images', () => {
    cy.resetDB();
    cy.loginAndVisit('/admin/campaigns');
    cy.request('PUT', '/api/profile/smtp', { smtp: [{ name: 'Inline image test', enabled: true,
      host: 'host.docker.internal', port: 6125, auth_protocol: 'none', from_email: 'inline-sender@example.com', tls_type: 'none' }] });
    const subject = `inline-image-e2e-${Date.now()}`;
    openCampaign(subject);
    const uploaded = [];
    cy.intercept('POST', '/api/media', (req) => req.continue((res) => {
      expect(res.statusCode).to.eq(200);
      uploaded.push(res.body.data);
      res.setDelay(1000);
    })).as('upload');
    cy.window().then((win) => {
      win.tinymce.editors[0].execCommand('mceImage');
    });
    cy.contains('.tox-dialog [role=tab]', 'Upload').click();
    cy.window().then((win) => {
      cy.get('.tox-dialog input[type=file]').selectFile({
        contents: Cypress.Buffer.from(imageData(win).split(',')[1], 'base64'),
        fileName: 'inline-direct.png', mimeType: 'image/png',
      }, { force: true });
    });
    cy.wait('@upload');
    cy.contains('.tox-dialog button', 'Save').click();
    cy.window().then((win) => {
      const editor = win.tinymce.editors[0];
      editor.dom.setAttrib(editor.getBody().querySelector('img'), 'alt', 'inline-direct');
      editor.selection.select(editor.getBody(), true);
      editor.selection.collapse(false);
      editor.insertContent(`<p><img alt="inline-pasted" src="${imageData(win)}"></p>`);
    });
    cy.intercept('PUT', '/api/campaigns/*').as('save');
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@upload');
    cy.wait('@save').then(({ request, response }) => {
      expect(request.body.body).not.to.match(/(?:blob:|data:image)/);
      expect(request.body.media).to.have.length(2);
      expect(response.statusCode).to.eq(200);
      expect(response.body.data.media.map((item) => item.id)).to.deep.eq(uploaded.map((item) => item.id));
    });
    cy.reload();
    cy.get('.tox-tinymce').should('be.visible');
    cy.window().then((win) => {
      win.addEventListener('message', (event) => {
        if (event.data?.type === 'inline-preview-images') win.inlinePreviewImages = event.data.images;
      });
    });
    cy.intercept('POST', '/api/campaigns/*/preview', (req) => req.continue((res) => {
      res.body += `<script>window.addEventListener('load', function () {
        parent.postMessage({type: 'inline-preview-images', images: Array.from(document.images).filter(function (image) {
          return image.alt.indexOf('inline-') === 0;
        }).map(function (image) {
          return {width: image.naturalWidth, src: image.getAttribute('src')};
        })}, '*');
      });</script>`;
    })).as('preview');
    cy.get('[data-cy=btn-preview]').click();
    cy.wait('@preview').its('response.statusCode').should('eq', 200);
    cy.window().should((win) => {
      expect(win.inlinePreviewImages).to.have.length(2);
      win.inlinePreviewImages.forEach((image) => expect(image.width, image.src).to.eq(32));
    });
    cy.get('.modal-card-foot button').click();
    cy.contains('.b-tabs nav a', 'Campaign').click();
    cy.get('[data-cy=campaign-test-section] input').type('john@example.com{enter}');
    cy.intercept('POST', '/api/campaigns/*/test').as('sendTest');
    cy.get('[data-cy=campaign-test-section] button').click();
    cy.wait('@sendTest').then(({ request, response }) => {
      expect(response.statusCode).to.eq(200);
      expect(request.body.media).to.have.length(2);
      expect(request.body.body).not.to.match(/(?:blob:|data:image)/);
    });
    cy.waitUntil(() => cy.request({ url: 'http://127.0.0.1:8265/api/v2/search',
      qs: { kind: 'containing', query: subject, start: 0, limit: 10 } }).then(({ body }) => body.items.length > 0));
    cy.request({ url: 'http://127.0.0.1:8265/api/v2/search',
      qs: { kind: 'containing', query: subject, start: 0, limit: 10 } }).then(({ body }) => {
      const raw = body.items[0].Raw.Data;
      expect(raw.toLowerCase()).not.to.contain('content-id:').and.not.to.contain('content-disposition: inline');
      expect(raw).not.to.contain('multipart/related').and.not.to.contain('multipart/mixed');
      const decoded = raw.replace(/=\r?\n/g, '').replace(/=([0-9a-f]{2})/gi, (_, hex) => String.fromCharCode(parseInt(hex, 16)));
      const links = [...decoded.matchAll(/<img[^>]+src="([^"]*\/email-media\/[^"]+)"/g)].map((match) => match[1]);
      expect(links).to.have.length(2);
      cy.window().then((win) => Promise.all(links.map(async (link) => {
        // Use the isolated backend and omit browser cookies: a recipient has
        // no application session. Decode the image without clicking a link.
        const response = await win.fetch(new URL(link).pathname, { credentials: 'omit' });
        expect(response.status).to.eq(200);
        expect(response.headers.get('content-type')).to.eq('image/png');
        const objectURL = win.URL.createObjectURL(await response.blob());
        const image = new win.Image();
        image.src = objectURL;
        await image.decode();
        expect(image.naturalWidth).to.eq(32);
        win.URL.revokeObjectURL(objectURL);
      })));
    });
    cy.visit('/admin/campaigns/media');
    cy.get('.media-files').should('contain', 'inline-direct.png');
  });

  it('keeps failed images editable and prevents preview, save, test send and start until retry succeeds', () => {
    openCampaign('inline-image-upload-failure');
    let failUpload = true;
    cy.intercept('POST', '/api/media', (req) => {
      if (failUpload) req.reply({ statusCode: 403, body: { message: 'Inline upload denied' } });
      else req.continue();
    }).as('upload');
    let saves = 0;
    let previews = 0;
    let starts = 0;
    let tests = 0;
    cy.intercept('PUT', '/api/campaigns/*', (req) => { saves += 1; req.continue(); }).as('save');
    cy.intercept('POST', '/api/campaigns/*/preview', (req) => { previews += 1; req.continue(); });
    cy.intercept('PUT', '/api/campaigns/*/status', (req) => { starts += 1; req.continue(); });
    cy.intercept('POST', '/api/campaigns/*/test', (req) => { tests += 1; req.continue(); });
    cy.window().then((win) => win.tinymce.editors[0].setContent(`<img src="${imageData(win)}" alt="failed-image">`));
    cy.wait('@upload');
    cy.get('[data-cy=btn-preview]').click();
    cy.wait('@upload');
    cy.get('.toast').should('contain', 'The image could not be uploaded');
    cy.get('#iframe').should('not.exist');
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@upload');
    cy.get('[data-cy=btn-start]').click();
    cy.get('.modal button.is-primary').click();
    cy.wait('@upload');
    cy.contains('.b-tabs nav a', 'Campaign').click();
    cy.get('[data-cy=campaign-test-section] input').type('john@example.com{enter}');
    cy.get('[data-cy=campaign-test-section] button').click();
    cy.wait('@upload');
    cy.then(() => {
      expect({ saves, previews, starts, tests }).to.deep.eq({ saves: 0, previews: 0, starts: 0, tests: 0 });
      failUpload = false;
    });
    cy.contains('.b-tabs nav a', 'Content').click();
    cy.get('.campaign > header [data-cy=btn-save]').click();
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.wait('@save').its('response.statusCode').should('eq', 200);
    cy.window().should((win) => {
      expect(win.tinymce.editors[0].getContent()).not.to.match(/(?:blob:|data:image)/);
    });
    cy.then(() => expect(saves).to.eq(1));
  });

  it('preserves literal HTML in plain-text previews opened from the campaign list', () => {
    cy.request('POST', '/api/campaigns', {
      name: 'plain-preview-regression', subject: 'plain-preview-regression', type: 'regular', content_type: 'plain',
      body: '<h1>Literal markup</h1>', customer_list_ids: [1], smtp_source: 'personal', smtp_rate_limit: 20,
    });
    cy.visit('/admin/campaigns');
    cy.contains('tbody tr', 'plain-preview-regression').find('[data-cy=btn-preview]').click();
    cy.get('#iframe').should('have.attr', 'sandbox', 'allow-scripts');
    cy.get('#iframe').invoke('attr', 'srcdoc').should('contain', '<pre>').and('contain', '&lt;h1&gt;Literal markup&lt;/h1&gt;');
    cy.get('.modal-card-foot button').click();
  });

  it('does not fetch unrelated private media for untrusted preview HTML', () => {
    openCampaign('unrelated-preview-image');
    let unrelatedURL;
    cy.window().then(async (win) => {
      const blob = await (await win.fetch(imageData(win))).blob();
      const data = new win.FormData();
      data.set('file', blob, 'unrelated-private-image.png');
      const response = await win.fetch('/api/media', { method: 'POST', body: data });
      expect(response.status).to.eq(200);
      unrelatedURL = (await response.json()).data.url;
      win.tinymce.editors[0].setContent(`<img src="${unrelatedURL}">`);
    });
    let authenticatedFileReads = 0;
    cy.intercept('GET', '/api/media/file/*', (req) => {
      if (req.headers['sec-fetch-dest'] === 'empty') authenticatedFileReads += 1;
      req.continue();
    });
    cy.get('[data-cy=btn-preview]').click();
    cy.get('#iframe').invoke('attr', 'srcdoc').should((html) => {
      expect(html).to.contain(unrelatedURL).and.not.to.contain('data:image/png');
    });
    cy.then(() => expect(authenticatedFileReads).to.eq(0));
    cy.get('.modal-card-foot button').click();
  });

  it('waits for a pasted image before previewing without saving the campaign', () => {
    openCampaign('unsaved-inline-preview');
    cy.intercept('POST', '/api/media', (req) => req.continue((res) => res.setDelay(1000))).as('upload');
    cy.window().then((win) => win.tinymce.editors[0].setContent(`<img src="${imageData(win)}" alt="unsaved-inline">`));
    cy.get('[data-cy=btn-preview]').click();
    cy.wait('@upload').its('response.statusCode').should('eq', 200);
    cy.get('#iframe').invoke('attr', 'srcdoc').should('contain', 'src="data:image/png;base64,');
    cy.location('pathname').then((pathname) => {
      cy.request(`/api/campaigns/${pathname.split('/').pop()}`).then(({ body }) => {
        expect(body.data.body).not.to.contain('<img');
        expect(body.data.media).to.have.length(0);
      });
    });
    cy.get('.modal-card-foot button').click();
  });
});
