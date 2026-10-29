<template>
  <div>
    <b-modal scroll="keep" @close="close" :aria-modal="true" :active="isVisible">
      <div>
        <div class="modal-card" style="width: auto">
          <header class="modal-card-head">
            <h4>{{ title }}</h4>
          </header>
        </div>
        <section expanded class="modal-card-body preview">
          <b-loading :active="isLoading" :is-full-page="false" />
          <iframe id="iframe" name="iframe" ref="iframe" :title="title" :srcdoc="previewHTML"
            @load="onLoaded" sandbox="allow-scripts" />
        </section>
        <footer class="modal-card-foot has-text-right">
          <b-button @click="close">
            {{ $t('globals.buttons.close') }}
          </b-button>
        </footer>
      </div>
    </b-modal>
  </div>
</template>

<script>
import { uris } from '../constants';

export default {
  name: 'CampaignPreview',

  props: {
    isPost: { type: Boolean, default: false },

    // Template or campaign ID.
    id: { type: Number, default: 0 },
    title: { type: String, default: '' },

    // campaign | template.
    type: { type: String, default: '' },

    // campaign | tx.
    templateType: { type: String, default: '' },

    archiveMeta: { type: String, default: null },

    nameFallback: { type: Object, default: null },
    body: { type: String, default: '' },
    media: { type: Array, default: () => [] },
    contentType: { type: String, default: '' },
    templateId: { type: [Number, null], default: null },
    autoTrackLinks: { type: [Boolean, null], default: null },
    isArchive: { type: Boolean, default: false },
  },

  data() {
    return {
      isVisible: true,
      isLoading: true,
      previewHTML: null,
    };
  },

  methods: {
    close() {
      this.$emit('close');
      this.isVisible = false;
    },

    // On iframe load, kill the spinner.
    onLoaded() {
      if (this.previewHTML !== null) {
        this.isLoading = false;
      }
    },

    async embedPreviewImages(html) {
      const allowedMedia = [...this.media];
      // List previews receive campaign media separately from wrapper-template
      // media. Load that exact authorized template so its private/shared images
      // can also be materialized inside the cookie-free sandbox.
      if (this.type === 'campaign' && this.templateId && this.contentType !== 'visual') {
        const selectedTemplate = await this.$api.getTemplate(this.templateId);
        allowedMedia.push(...(selectedTemplate.media || []));
      }
      const template = document.createElement('template');
      template.innerHTML = html;
      const sources = [...new Set(Array.from(template.content.querySelectorAll('img')).map((image) => image.getAttribute('src')).filter(Boolean))];
      const embedded = new Map();
      await Promise.all(sources.map(async (source) => {
        const url = new URL(source, window.location.href);
        if (url.origin !== window.location.origin || !/^\/(?:api\/media\/file|uploads)\//.test(url.pathname)) return;
        const match = url.pathname.match(/^\/api\/media\/file\/(\d+)\/([^/]+)$/);
        const filename = decodeURIComponent(url.pathname.split('/').pop());
        const media = allowedMedia.find((item) => item.id && item.filename === filename && (!match || Number(item.id) === Number(match[1])));
        if (!media) return;
        // Read protected files with the parent's authenticated workspace request.
        // The sandbox receives only these authorized bytes, never a session cookie.
        // A Super Admin may preview a campaign from another workspace while the
        // media sits in a folder outside the current one; the explicit platform
        // scope lets the existing read-only catalog endpoint serve those bytes.
        const mediaURL = `/api/media/file/${media.id}/${encodeURIComponent(media.filename)}`;
        const blob = await this.$api.getMediaFile(
          this.$isPlatformAdmin() ? `${mediaURL}?scope=all_owners` : mediaURL,
        );
        const dataURL = await new Promise((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => resolve(reader.result);
          reader.onerror = () => reject(reader.error);
          reader.readAsDataURL(blob);
        });
        embedded.set(source, dataURL);
      }));
      return html.replace(/(<img\b[^>]*?\ssrc\s*=\s*)(?:"([^"]*)"|'([^']*)'|([^\s"'=<>`]+))/gi, (match, prefix, double, single, unquoted) => {
        const decoder = document.createElement('template');
        decoder.innerHTML = double ?? single ?? unquoted;
        const source = decoder.content.textContent;
        return embedded.has(source) ? `${prefix}"${embedded.get(source)}"` : match;
      });
    },
  },

  computed: {
    previewURL() {
      let uri = 'about:blank';

      if (this.type === 'campaign') {
        uri = this.isArchive ? uris.previewCampaignArchive : uris.previewCampaign;
      } else if (this.type === 'template') {
        if (this.id) {
          uri = uris.previewTemplate;
        } else {
          uri = uris.previewRawTemplate;
        }
      }

      return uri.replace(':id', this.id);
    },
  },

  async mounted() {
    let data = null;
    if (this.isPost) {
      data = new URLSearchParams();
      if (this.templateId) data.set('template_id', this.templateId);
      if (this.contentType) data.set('content_type', this.contentType);
      if (this.templateType) data.set('template_type', this.templateType);
      if (this.archiveMeta) data.set('archive_meta', this.archiveMeta);
      if (this.autoTrackLinks !== null) data.set('auto_track_links', this.autoTrackLinks);
      if (this.nameFallback) data.set('name_fallback', JSON.stringify(this.nameFallback));
      if (this.body) data.set('body', this.body);
    }
    try {
      const response = await this.$api.renderPreview(this.previewURL, data);
      let html = response.body;
      if (response.contentType?.startsWith('text/plain')) {
        const pre = document.createElement('pre');
        pre.textContent = html;
        html = pre.outerHTML;
      }
      this.previewHTML = await this.embedPreviewImages(html);
    } catch (error) {
      this.isLoading = false;
      this.$utils.toast(this.$t('globals.messages.errorFetching', { name: this.$t('campaigns.preview'), error: error.toString() }), 'is-danger');
    }
  },
};
</script>
